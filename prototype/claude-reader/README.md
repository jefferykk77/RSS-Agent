# Claude Reader Prototype

Standalone prototype for manual approval. It does not call the production API and does not replace the Go-embedded frontend.

Run from this folder:

```powershell
python -m http.server 8792
```

Open `http://127.0.0.1:8792/`.

Interactive paths:

- Open and close the source/history drawer.
- Switch between all, recommended and saved items.
- Search the queue and select an article.
- Toggle feedback and saved state.
- Open the feed dialog.
- Simulate the run progress state.
