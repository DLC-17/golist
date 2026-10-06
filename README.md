# GoList

A lean, zero-dependency Go CLI tool that automates eBay listing generation from local photos using Gemini multimodal AI.

Built following the **Ponytail** engineering philosophy: stdlib first, filesystem-as-state, zero database overhead, and safe human-in-the-loop review.

## Features
- **Multimodal AI Vision:** Uses **Gemini 2.5 Flash** via standard HTTP requests to inspect photos, extract item specifics, read tags/serial numbers, and generate structured listing drafts.
- **Filesystem-as-State:** No database or migrations. Tracks batches across `dump/` → `processing/` → `drafts/` → `published/`.
- **Safe Review Verification:** Review and edit drafts locally in your editor before spending money or touching live eBay APIs.
- **eBay Quota Compliance:** Strict 50 requests/min token-bucket rate limiter with automatic pause and exponential backoff on HTTP 429.

## Quick Start

### 1. Build
```bash
go build -o golist .
```

### 2. Configure
Copy `.env.example` to `.env` and fill in your keys:
```bash
cp .env.example .env
```

### 3. Workflow
1. **Drop Images:** Create a folder inside `data/dump/` (e.g., `data/dump/vintage_jacket/`) and drop your photos there.
2. **Process Batches:**
   ```bash
   ./golist process
   ```
   Or process a specific directory:
   ```bash
   ./golist process /path/to/photos
   ```
3. **Review Draft:**
   ```bash
   ./golist review vintage_jacket
   # To edit specifics/price in your $EDITOR:
   ./golist review vintage_jacket --edit
   # To approve for publishing:
   ./golist review vintage_jacket --approve
   ```
4. **Publish to eBay:**
   ```bash
   ./golist publish vintage_jacket
   # Or publish all approved drafts:
   ./golist publish --all
   # Or test dry-run:
   ./golist publish vintage_jacket --dry-run
   ```
5. **Check Pipeline Status:**
   ```bash
   ./golist status
   ```
