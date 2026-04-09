# TO-DO Items

*Note:* These items are ideas for future enhancements. Their suitability needs
to be assessed as part of the initial work, and inclusion here does not 
represent a roadmap or any guarantee that any features will actually be
implemented.

## LLM Support

1. Add support for use of Google Gemini as the LLM provider.
2. Add support for use of OpenAI API-compatible local LLM providers, such as
    LM Studio, Docker Model Runner, and EXO. These should just work if 
    configured as OpenAI, but without a requirement for an API key.
3. Add support for arbitrary request headers to be added to LLM request calls
    to support servers such as Portkey which requires addition of headers such
    as `x-portkey-provider: openai`
    
## Integration

1. pgedge-vectorizer will support BM25 through generated sparse vectors in 
    it's next release. We can make use of that as an optional alternative to
    our built in hybrid search.