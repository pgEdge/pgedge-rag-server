# API Keys

You can provide API keys in different locations; this allows different pipelines to use different API keys or accounts while sharing common defaults.

## Specifying the Path to API Key Files

You can specify the path to API key files containing API keys in several locations.  The RAG server will search for your API key in the order these locations are listed below.

The first location searched for an API key is within the `pipelines` section of your configuration file:

```yaml
pipelines:
  - name: "production"
    api_keys:
      anthropic: "/etc/pgedge/keys/prod-anthropic.key"
    # ... other pipeline config
```

Use the following fields within the configuration file:

| Field       | Description                           |
|-------------|---------------------------------------|
| `anthropic` | Path to file containing Anthropic key |
| `gemini`    | Path to file containing Gemini key    |
| `openai`    | Path to file containing OpenAI key    |
| `voyage`    | Path to file containing Voyage key    |

If the RAG server does not locate an API key in the pipelines section, it searches in the `defaults` section of the configuration file:

```yaml
defaults:
  api_keys:
    openai: "/etc/pgedge/keys/default-openai.key"
    anthropic: "/etc/pgedge/keys/default-anthropic.key"
```

You also have the option of providing key values in a globally accessible `api_keys` section of the configuration file.  Keys provided in the `api_keys` section apply to all pipelines unless overridden by values in the `pipelines` or `defaults` sections.

```yaml
api_keys:
  anthropic: "/etc/pgedge/keys/anthropic.key"
  voyage: "/etc/pgedge/keys/voyage.key"
  openai: "~/secrets/openai-api-key"
```

!!! hint

    Paths support `~` expansion for the home directory. Each file should contain only the API key (no other content).

**Providing API Values in Environment Variables or Default File Locations**

If you don't specify the API key values in the configuration file, the RAG server will check
environment variables for key values:

```bash
export OPENAI_API_KEY="sk-..."
export ANTHROPIC_API_KEY="sk-ant-..."
export VOYAGE_API_KEY="pa-..."
export GEMINI_API_KEY="your-gemini-key"
```

If neither configuration paths nor environment variables are set, the server looks for API keys in these default locations:

| Provider  | File Location           |
|-----------|-------------------------|
| OpenAI    | `~/.openai-api-key`     |
| Anthropic | `~/.anthropic-api-key`  |
| Gemini    | `~/.gemini-api-key`     |
| Voyage    | `~/.voyage-api-key`     |

## Gemini Configuration

Google Gemini uses API key authentication. The key is sent as a
query parameter with each request. Default models are
`gemini-2.0-flash` for completion and `text-embedding-004` for
embeddings.

```yaml
embedding_llm:
  provider: "gemini"
  model: "text-embedding-004"
rag_llm:
  provider: "gemini"
  model: "gemini-2.0-flash"
```

The default base URL is
`https://generativelanguage.googleapis.com`. To use a different
endpoint, set `base_url` in the LLM configuration:

```yaml
rag_llm:
  provider: "gemini"
  model: "gemini-2.0-flash"
  base_url: "https://your-gemini-proxy.example.com"
```

## OpenAI-Compatible Local Providers

When using OpenAI-compatible local LLM servers such as
[LM Studio](https://lmstudio.ai),
[Docker Model Runner](https://docs.docker.com/ai/model-runner/),
or [EXO](https://github.com/exo-explore/exo), the API key is
optional. Set `base_url` to point at the local server:

```yaml
embedding_llm:
  provider: "openai"
  model: "nomic-embed-text"
  base_url: "http://localhost:1234/v1"
rag_llm:
  provider: "openai"
  model: "llama3"
  base_url: "http://localhost:1234/v1"
```

No API key is required for local servers. If a key is provided
(via config, environment variable, or default file location), it
will be sent as a Bearer token as usual.

## Ollama Configuration

Ollama runs locally and does not require API keys. By default,
it connects to `http://localhost:11434`. To use a different URL,
set the `base_url` in the LLM configuration:

```yaml
embedding_llm:
  provider: "ollama"
  model: "nomic-embed-text"
  base_url: "http://my-ollama-server:11434"
```

Alternatively, set the `OLLAMA_HOST` environment variable:

```bash
export OLLAMA_HOST="http://my-ollama-server:11434"
```

