# GoList

A lean, zero-dependency Go CLI tool that automates eBay listing generation from local image batches using Gemini multimodal AI.

Built following the **Ponytail** engineering philosophy: standard library first, filesystem-as-state, zero database overhead, and safe human-in-the-loop draft review.

---

## Features

- **Multimodal AI Vision:** Leverages **Gemini 2.5 Flash** (via stdlib `net/http`) to inspect multi-angle photos, extract brand labels/model numbers/condition flaws via OCR, and generate structured listing drafts.
- **Filesystem-as-State:** Zero database dependencies or migrations. Batches transition cleanly across:
  `data/dump/` → `data/processing/` → `data/drafts/` → `data/published/`
- **Mandatory Review Guardrail:** Generates local `item.json` drafts for human verification and price adjustments before spending listing fees or touching live eBay inventory.
- **eBay Quota & Rate Limit Protection:** Hard token-bucket ceiling of **50 requests per minute**. On HTTP 429 rate limits, GoList pauses with exponential backoff without destroying local drafts.
- **Crash Recovery:** Automatically detects and recovers batches interrupted in `processing/` on the next run.

---

## Prerequisites

- **Go:** 1.22 or higher (`go version`)
- **API Keys (for live operations):**
  - Google Gemini API key ([Google AI Studio](https://aistudio.google.com/))
  - eBay Developer account ([eBay Developer Portal](https://developer.ebay.com/)) with Inventory API access

---

## Installation & Build

Clone the repository and compile the binary:

```bash
git clone https://github.com/DLC-17/golist.git
cd golist
go build -o golist .
```

To run unit tests:

```bash
go test -v ./...
```

---

## Configuration

Copy `.env.example` to `.env` in the working directory:

```bash
cp .env.example .env
```

### Environment Variables

| Variable | Description | Default |
| :--- | :--- | :--- |
| `GEMINI_API_KEY` | Google Gemini API key | *(Required for live AI)* |
| `GEMINI_MODEL` | Gemini vision model to use | `gemini-2.5-flash` |
| `EBAY_CLIENT_ID` | eBay Developer App ID | *(Required for live publish)* |
| `EBAY_CLIENT_SECRET` | eBay Developer Cert ID | *(Required for live publish)* |
| `EBAY_REFRESH_TOKEN` | eBay User OAuth Refresh Token | *(Required for live publish)* |
| `EBAY_ENV` | Target eBay environment (`sandbox` or `production`) | `sandbox` |
| `DATA_DIR` | Base storage folder | `./data` |
| `GOLIST_MOCK_AI` | Set to `1` to generate realistic mock drafts offline | `0` |

---

## Complete Workflow

### 1. Ingest Photos
Place a folder of item photos into `data/dump/` (e.g., `data/dump/batch_shoes_01/` with `.jpg`, `.png`, or `.webp` files):

```bash
mkdir -p data/dump/batch_shoes_01
cp ~/Downloads/photos/*.jpg data/dump/batch_shoes_01/
```

### 2. Process Batches
Run the processor to atomically claim the batch, call Gemini Vision, and write the draft:

```bash
./golist process
```

- **Process an external folder directly:**
  ```bash
  ./golist process /path/to/my_items
  ```
- **Run in watch mode (polls `data/dump/` every 5 seconds):**
  ```bash
  ./golist process --watch
  ```

### 3. Review & Edit Draft
Inspect the generated draft details:

```bash
./golist review batch_shoes_01
```

- **Edit in your text editor** (uses `$EDITOR` or `nano`):
  ```bash
  ./golist review batch_shoes_01 --edit
  ```
- **Approve for publishing:**
  ```bash
  ./golist review batch_shoes_01 --approve
  ```

### 4. Publish to eBay
Publish approved drafts to eBay under the 50 req/min rate limit:

```bash
# Publish a specific approved batch:
./golist publish batch_shoes_01

# Publish all approved drafts in drafts/:
./golist publish --all

# Publish without requiring APPROVED status:
./golist publish batch_shoes_01 --force
```

- **Dry-Run Simulation (no API keys required):**
  ```bash
  ./golist publish batch_shoes_01 --dry-run
  ```

Upon success, the batch directory is moved to `data/published/batch_shoes_01/` with the eBay listing ID recorded in `item.json`.

### 5. Check Pipeline Status
View active batch counts and current rate limiter token status at any time:

```bash
./golist status
```

Example output:
```text
GoList Pipeline Status:
  • Dump (Pending Ingestion): 0 batches
  • Processing (Active):      0 batches
  • Drafts (Pending Review):  2 batches
  • Published:                5 listings
  • Rate Limiter:             tokens: 50.0/50, paused: false
```

---

## Offline Testing & Verification

You can test the full pipeline locally without configured API keys:

```bash
# 1. Create a dummy image batch
mkdir -p data/dump/test_item
touch data/dump/test_item/photo1.jpg data/dump/test_item/photo2.jpg

# 2. Process using mock AI
GOLIST_MOCK_AI=1 ./golist process

# 3. Review and approve
./golist review test_item --approve

# 4. Dry-run publish
./golist publish test_item --dry-run

# 5. Verify status
./golist status
```

---

## License

MIT
