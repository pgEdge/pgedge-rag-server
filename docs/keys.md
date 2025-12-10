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
```

If neither configuration paths nor environment variables are set, the server looks for API keys in these default locations:

| Provider  | File Location           |
|-----------|-------------------------|
| OpenAI    | `~/.openai-api-key`     |
| Anthropic | `~/.anthropic-api-key`  |
| Voyage    | `~/.voyage-api-key`     |

## Ollama Configuration

Ollama runs locally and does not require API keys. By default, it connects to
`http://localhost:11434`. To use a different URL, set the `OLLAMA_HOST`
environment variable:

```bash
export OLLAMA_HOST="http://my-ollama-server:11434"
```

