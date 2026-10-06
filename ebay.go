package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type EbayClient struct {
	clientID     string
	clientSecret string
	refreshToken string
	accessToken  string
	tokenExpiry  time.Time
	baseURL      string
	authURL      string
	limiter      *RateLimiter
	httpClient   *http.Client
	dryRun       bool
}

func NewEbayClient(cfg Config, limiter *RateLimiter, dryRun bool) *EbayClient {
	baseURL := "https://api.sandbox.ebay.com"
	authURL := "https://api.sandbox.ebay.com/identity/v1/oauth2/token"
	if strings.ToLower(cfg.EbayEnv) == "production" {
		baseURL = "https://api.ebay.com"
		authURL = "https://api.ebay.com/identity/v1/oauth2/token"
	}

	return &EbayClient{
		clientID:     cfg.EbayClientID,
		clientSecret: cfg.EbayClientSecret,
		refreshToken: cfg.EbayRefreshToken,
		baseURL:      baseURL,
		authURL:      authURL,
		limiter:      limiter,
		httpClient:   &http.Client{Timeout: 30 * time.Second},
		dryRun:       dryRun,
	}
}

// PublishDraft creates inventory item, offer, and publishes it on eBay.
// Returns the eBay listing ID.
func (e *EbayClient) PublishDraft(ctx context.Context, draft *ItemDraft) (string, error) {
	if e.dryRun || e.clientID == "" {
		// Simulated publish for offline/dry-run testing
		if err := e.limiter.Acquire(ctx); err != nil {
			return "", err
		}
		listingID := fmt.Sprintf("SIM-EBAY-%d", time.Now().UnixNano()%100000000)
		return listingID, nil
	}

	if err := e.ensureAccessToken(ctx); err != nil {
		return "", fmt.Errorf("failed to authenticate with eBay: %w", err)
	}

	sku := fmt.Sprintf("GOLIST-%s", draft.ID)

	// Step 1: Create or replace inventory item
	itemPayload := map[string]interface{}{
		"availability": map[string]interface{}{
			"shipToLocationAvailability": map[string]interface{}{
				"quantity": 1,
			},
		},
		"condition": mapCondition(draft.Condition),
		"product": map[string]interface{}{
			"title":       draft.Title,
			"description": draft.Description,
			"aspects":     formatAspects(draft.ItemSpecifics),
		},
	}

	itemURL := fmt.Sprintf("%s/sell/inventory/v1/inventory_item/%s", e.baseURL, sku)
	if err := e.doWithRetry(ctx, "PUT", itemURL, itemPayload, nil); err != nil {
		return "", fmt.Errorf("failed to create inventory item: %w", err)
	}

	// Step 2: Create offer
	offerPayload := map[string]interface{}{
		"sku":           sku,
		"marketplaceId": "EBAY_US",
		"format":        "FIXED_PRICE",
		"categoryId":    draft.CategoryID,
		"pricingSummary": map[string]interface{}{
			"price": map[string]interface{}{
				"value":    fmt.Sprintf("%.2f", draft.SuggestedPrice),
				"currency": "USD",
			},
		},
	}
	var offerResp struct {
		OfferID string `json:"offerId"`
	}
	offerURL := fmt.Sprintf("%s/sell/inventory/v1/offer", e.baseURL)
	if err := e.doWithRetry(ctx, "POST", offerURL, offerPayload, &offerResp); err != nil {
		return "", fmt.Errorf("failed to create offer: %w", err)
	}

	// Step 3: Publish offer
	var publishResp struct {
		ListingID string `json:"listingId"`
	}
	publishURL := fmt.Sprintf("%s/sell/inventory/v1/offer/%s/publish", e.baseURL, offerResp.OfferID)
	if err := e.doWithRetry(ctx, "POST", publishURL, nil, &publishResp); err != nil {
		return "", fmt.Errorf("failed to publish offer: %w", err)
	}

	return publishResp.ListingID, nil
}

// doWithRetry executes HTTP request respecting the rate limiter and backoff on 429
func (e *EbayClient) doWithRetry(ctx context.Context, method, url string, reqPayload interface{}, respDest interface{}) error {
	maxAttempts := 5
	backoff := 1 * time.Second

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		// Enforce 50 calls/min rate limit ceiling
		if err := e.limiter.Acquire(ctx); err != nil {
			return err
		}

		var bodyReader io.Reader
		if reqPayload != nil {
			b, err := json.Marshal(reqPayload)
			if err != nil {
				return err
			}
			bodyReader = bytes.NewReader(b)
		}

		req, err := http.NewRequestWithContext(ctx, method, url, bodyReader)
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+e.accessToken)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Content-Language", "en-US")

		resp, err := e.httpClient.Do(req)
		if err != nil {
			if attempt == maxAttempts {
				return err
			}
			time.Sleep(backoff)
			backoff *= 2
			continue
		}

		respBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		if resp.StatusCode == http.StatusTooManyRequests {
			// Extract Retry-After if present
			retryWait := backoff
			if ra := resp.Header.Get("Retry-After"); ra != "" {
				if sec, err := strconv.Atoi(ra); err == nil && sec > 0 {
					retryWait = time.Duration(sec) * time.Second
				}
			}
			// Apply backoff across all workers via limiter
			e.limiter.Backoff(retryWait)
			if attempt == maxAttempts {
				return fmt.Errorf("rate limit exceeded after %d retries: %s", maxAttempts, string(respBody))
			}
			time.Sleep(retryWait)
			backoff *= 2
			continue
		}

		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			if respDest != nil && len(respBody) > 0 {
				if err := json.Unmarshal(respBody, respDest); err != nil {
					return fmt.Errorf("failed to parse response json: %w", err)
				}
			}
			return nil
		}

		// Non-retryable error
		if resp.StatusCode < 500 {
			return fmt.Errorf("eBay API error (HTTP %d): %s", resp.StatusCode, string(respBody))
		}

		// Server 5xx error retry
		if attempt == maxAttempts {
			return fmt.Errorf("eBay server error (HTTP %d): %s", resp.StatusCode, string(respBody))
		}
		time.Sleep(backoff)
		backoff *= 2
	}

	return fmt.Errorf("request exceeded maximum retry attempts")
}

func (e *EbayClient) ensureAccessToken(ctx context.Context) error {
	if e.accessToken != "" && time.Now().Before(e.tokenExpiry) {
		return nil
	}

	data := url.Values{}
	data.Set("grant_type", "refresh_token")
	data.Set("refresh_token", e.refreshToken)
	data.Set("scope", "https://api.ebay.com/oauth/api_scope/sell.inventory")

	req, err := http.NewRequestWithContext(ctx, "POST", e.authURL, strings.NewReader(data.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	authHeader := base64.StdEncoding.EncodeToString([]byte(e.clientID + ":" + e.clientSecret))
	req.Header.Set("Authorization", "Basic "+authHeader)

	resp, err := e.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("oauth token exchange failed (HTTP %d): %s", resp.StatusCode, string(b))
	}

	var tokenResp struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return err
	}

	e.accessToken = tokenResp.AccessToken
	e.tokenExpiry = time.Now().Add(time.Duration(tokenResp.ExpiresIn-60) * time.Second)
	return nil
}

func mapCondition(c string) string {
	switch strings.ToUpper(c) {
	case "NEW":
		return "NEW"
	case "LIKE_NEW":
		return "LIKE_NEW"
	case "USED_GOOD":
		return "USED_GOOD"
	case "FOR_PARTS":
		return "FOR_PARTS_OR_NOT_WORKING"
	default:
		return "USED_EXCELLENT"
	}
}

func formatAspects(specifics map[string]string) map[string][]string {
	res := make(map[string][]string)
	for k, v := range specifics {
		res[k] = []string{v}
	}
	return res
}
