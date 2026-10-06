package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type AICategorizer struct {
	apiKey     string
	model      string
	httpClient *http.Client
}

func NewAICategorizer(apiKey, model string) *AICategorizer {
	if model == "" {
		model = "gemini-2.5-flash"
	}
	return &AICategorizer{
		apiKey: apiKey,
		model:  model,
		httpClient: &http.Client{
			Timeout: 60 * time.Second,
		},
	}
}

// GenerateDraft analyzes the provided image files and returns a structured ItemDraft
func (ai *AICategorizer) GenerateDraft(ctx context.Context, batchID string, imagePaths []string) (*ItemDraft, error) {
	if len(imagePaths) == 0 {
		return nil, fmt.Errorf("no images provided for batch %s", batchID)
	}

	if os.Getenv("GOLIST_MOCK_AI") == "1" || ai.apiKey == "mock" {
		return mockDraft(batchID, imagePaths), nil
	}

	if ai.apiKey == "" {
		return nil, fmt.Errorf("GEMINI_API_KEY is not configured; set GEMINI_API_KEY or GOLIST_MOCK_AI=1 for offline testing")
	}

	// Prepare Gemini API request parts
	type inlineData struct {
		MimeType string `json:"mime_type"`
		Data     string `json:"data"`
	}
	type part struct {
		Text       string      `json:"text,omitempty"`
		InlineData *inlineData `json:"inline_data,omitempty"`
	}
	type content struct {
		Role  string `json:"role"`
		Parts []part `json:"parts"`
	}
	type genConfig struct {
		ResponseMimeType string `json:"responseMimeType"`
	}
	type geminiRequest struct {
		Contents         []content `json:"contents"`
		GenerationConfig genConfig `json:"generationConfig"`
	}

	systemInstruction := `You are an expert eBay seller assistant. Analyze the provided photos of an item.
Inspect all labels, tags, logos, model numbers, serial numbers, barcodes, materials, and condition flaws.
Respond ONLY with a valid JSON object matching this schema:
{
  "title": "Searchable eBay title under 80 characters, Brand + Model + Key Features + Condition",
  "category_id": "Estimated eBay category ID if known or numeric string",
  "category_name": "Standard eBay category name",
  "condition": "NEW, LIKE_NEW, USED_EXCELLENT, USED_GOOD, or FOR_PARTS",
  "suggested_price": 29.99,
  "item_specifics": {
    "Brand": "Extracted brand",
    "Model": "Extracted model or N/A",
    "Type": "Item type",
    "Color": "Primary color",
    "Condition Details": "Specific flaws or wear noticed"
  },
  "description": "Professional eBay item description including bullet points of condition, flaws, and specs."
}`

	reqBody := geminiRequest{
		Contents: []content{
			{
				Role: "user",
				Parts: []part{
					{Text: systemInstruction},
				},
			},
		},
		GenerationConfig: genConfig{
			ResponseMimeType: "application/json",
		},
	}

	for _, imgPath := range imagePaths {
		data, err := os.ReadFile(imgPath)
		if err != nil {
			return nil, fmt.Errorf("failed to read image %s: %w", imgPath, err)
		}
		ext := strings.ToLower(filepath.Ext(imgPath))
		mime := "image/jpeg"
		if ext == ".png" {
			mime = "image/png"
		} else if ext == ".webp" {
			mime = "image/webp"
		}

		reqBody.Contents[0].Parts = append(reqBody.Contents[0].Parts, part{
			InlineData: &inlineData{
				MimeType: mime,
				Data:     base64.StdEncoding.EncodeToString(data),
			},
		})
	}

	reqBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal Gemini request: %w", err)
	}

	url := fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models/%s:generateContent?key=%s",
		ai.model, ai.apiKey)

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(reqBytes))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := ai.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("Gemini API request failed: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Gemini API error (HTTP %d): %s", resp.StatusCode, string(bodyBytes))
	}

	rawJSON, err := extractGeminiText(bodyBytes)
	if err != nil {
		return nil, err
	}

	draft, err := parseDraftJSON(rawJSON)
	if err != nil {
		return nil, fmt.Errorf("failed to parse AI JSON response: %w (raw: %s)", err, rawJSON)
	}

	draft.ID = batchID
	var imgBasenames []string
	for _, p := range imagePaths {
		imgBasenames = append(imgBasenames, filepath.Base(p))
	}
	draft.Images = imgBasenames
	draft.Status = "DRAFT_PENDING_REVIEW"

	return draft, nil
}

func extractGeminiText(respBytes []byte) (string, error) {
	var parsed struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
	}

	if err := json.Unmarshal(respBytes, &parsed); err != nil {
		return "", fmt.Errorf("invalid Gemini response envelope: %w", err)
	}

	if len(parsed.Candidates) == 0 || len(parsed.Candidates[0].Content.Parts) == 0 {
		return "", fmt.Errorf("empty candidate in Gemini response")
	}

	return parsed.Candidates[0].Content.Parts[0].Text, nil
}

func parseDraftJSON(raw string) (*ItemDraft, error) {
	clean := strings.TrimSpace(raw)
	// Strip optional markdown code fences if present
	if strings.HasPrefix(clean, "```json") {
		clean = strings.TrimPrefix(clean, "```json")
	} else if strings.HasPrefix(clean, "```") {
		clean = strings.TrimPrefix(clean, "```")
	}
	clean = strings.TrimSuffix(clean, "```")
	clean = strings.TrimSpace(clean)

	var draft ItemDraft
	if err := json.Unmarshal([]byte(clean), &draft); err != nil {
		return nil, err
	}

	// Enforce 80 char eBay title constraint
	if len(draft.Title) > 80 {
		draft.Title = draft.Title[:80]
	}

	return &draft, nil
}

func mockDraft(batchID string, imagePaths []string) *ItemDraft {
	var imgBasenames []string
	for _, p := range imagePaths {
		imgBasenames = append(imgBasenames, filepath.Base(p))
	}
	title := fmt.Sprintf("Vintage Item - Batch %s", batchID)
	if len(title) > 80 {
		title = title[:80]
	}
	return &ItemDraft{
		ID:             batchID,
		Title:          title,
		CategoryID:     "175672",
		CategoryName:   "Collectibles & Art",
		Condition:      "USED_EXCELLENT",
		SuggestedPrice: 34.50,
		ItemSpecifics: map[string]string{
			"Brand":     "Authentic Vintage",
			"Condition": "Pre-owned, tested, clean",
			"Batch":     batchID,
		},
		Description: "Detailed inspection completed. Item is in used excellent condition with minimal wear.",
		Images:      imgBasenames,
		Status:      "DRAFT_PENDING_REVIEW",
	}
}
