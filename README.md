# Vecura

A local desktop app for organizing and searching your AI-generated image collection by meaning, not just filename.

Vecura reads the prompt that's already baked into your images (Automatic1111/Forge, ComfyUI, NovelAI, EXIF, whatever) and lets you search your whole library semantically using an embedding model of your choice. Everything — images, thumbnails, database — stays on your machine. The only thing that goes over the network is the search text itself, sent to whichever embedding provider you configure.

## Features

- Hybrid search: keyword matching always works, semantic search kicks in once you've hooked up a model
- Automatic prompt extraction from PNG/JPEG metadata, with the generation junk (negative prompt, sampler, seed, etc.) cleaned out
- Use any OpenAI-compatible embedding provider — OpenAI, OpenRouter, LocalAI, Ollama, whatever you've got
- Fast incremental scanning — re-scanning a folder only touches new or changed files
- View the full prompt for any image in a popup, select and copy it
- Frameless, translucent native window (built with Wails), virtualized image grid so huge libraries stay smooth

## Getting started

You'll need Go 1.26+, Node 18+, and the [Wails CLI](https://wails.io/docs/gettingstarted/installation).

```bash
wails dev      # run with hot reload
wails build    # build a production binary into build/bin/
```

Then in the app: open **Settings**, pick a provider, drop in an API key (or set `OPENAI_API_KEY` / `OPENROUTER_API_KEY` as an env var instead), pick a model, and choose a folder to scan.

## Where your data lives

Everything lives under `~/.vecura/`:

- `gallery.db` — the SQLite database (images, prompts, tags, embeddings, search history)
- `thumbnails/` — generated thumbnails
- `config.json` — your settings (provider, model, last folder — API keys too, if you pasted them into the UI instead of using an env var)
- `window.json` — last window size

**Settings → Clear Database** wipes all of the above except registered providers/models.

## Donations

If Vecura is useful to you, consider supporting its development:

- **TRC20:** `TRjed7YN5kfgxSCL3RU7m1nXR8DXPzAt9o`
- **OPBNB:** `0x9Fe24f445684077f65b6C9160Effc3Ad9634C5a8`
- **TON:** `UQBGBc_dO65UacmmB8AyWqZiZxP21msy0hxvt2nz4Ms8pPcX`

## License

[MIT](LICENSE)
