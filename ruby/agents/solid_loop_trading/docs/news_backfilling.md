# News Backfilling from Telegram

Process for populating the database with historical news for backtesting trading agents.

## Import Process

1. **Export from Telegram Desktop**:
   - Open the desired channel.
   - Click `...` (menu) -> `Export chat history`.
   - Uncheck all media files (photos, videos, etc.).
   - Select **JSON** format.
   - Set the date range (e.g., all of March 2026).
   - Click `Export`.

2. **Load into the project**:
   - Place the resulting file (e.g., `result.json`) in the `storage/external/` folder.

3. **Run the import**:
   - Execute the Rake task:
     ```bash
     bin/rake "news:import_telegram[storage/external/WuBlockchainNews.json]"
     ```

## Recommended Channels for Trading

For quality market coverage, it is recommended to download history from the following channels:

| Channel | Topic | Link |
| :--- | :--- | :--- |
| **Wu Blockchain News** | Insider info, deals, technical outages | `@WuBlockchain` |
| **Whale Alert** | Large crypto transfers (liquidity) | `@WhaleAlert` |
| **Watcher.Guru** | Fast macroeconomic news (Fed, CPI) | `@WatcherGuru` |
| **The Block** | In-depth industry analytics | `@TheBlockNews` |
| **CoinTelegraph** | General crypto market news | `@cointelegraph` |

## Technical Details
The importer automatically:
- Merges text blocks (links, bold text) into a single content string.
- Saves the original `published_at` in UTC.
- Avoids duplicates by message ID and source.
