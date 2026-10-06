package main

import (
	"testing"
)

func TestParseDraftJSON(t *testing.T) {
	raw := "```json\n" + `{
		"title": "Super Long Nike Air Max Running Shoes Men Size 10.5 Vintage Rare Retro Classic Series Limited Edition Extra Words That Exceed Eighty Characters",
		"category_id": "15709",
		"category_name": "Men's Athletic Shoes",
		"condition": "USED_EXCELLENT",
		"suggested_price": 79.99,
		"item_specifics": {
			"Brand": "Nike",
			"Color": "White"
		},
		"description": "Clean vintage shoes"
	}` + "\n```"

	draft, err := parseDraftJSON(raw)
	if err != nil {
		t.Fatalf("failed to parse draft: %v", err)
	}

	if len(draft.Title) > 80 {
		t.Fatalf("title not truncated to 80 chars, got length %d: %s", len(draft.Title), draft.Title)
	}
	if draft.CategoryID != "15709" {
		t.Errorf("expected category 15709, got %s", draft.CategoryID)
	}
	if draft.SuggestedPrice != 79.99 {
		t.Errorf("expected price 79.99, got %f", draft.SuggestedPrice)
	}
	if draft.ItemSpecifics["Brand"] != "Nike" {
		t.Errorf("expected brand Nike, got %s", draft.ItemSpecifics["Brand"])
	}
}

func TestExtractGeminiText(t *testing.T) {
	sampleResp := []byte(`{
		"candidates": [
			{
				"content": {
					"parts": [
						{
							"text": "{\"title\": \"Vintage Camera\"}"
						}
					]
				}
			}
		]
	}`)

	text, err := extractGeminiText(sampleResp)
	if err != nil {
		t.Fatalf("failed to extract gemini text: %v", err)
	}
	if text != `{"title": "Vintage Camera"}` {
		t.Fatalf("unexpected text: %s", text)
	}
}
