# pgEdge RAG Server

The pgEdge RAG server is a simple API server for performing Retrieval-Augmented Generation (RAG)
of text based on content from a Postgres database using
[pgvector](https://github.com/pgvector/pgvector).

## What is RAG?

Retrieval-Augmented Generation combines information retrieval with
generative AI to produce accurate, grounded responses. Instead of relying
solely on an LLM's training data, RAG:

1. retrieves relevant documents from a knowledge base.
2. provides those documents as context to the LLM.
3. generates an answer based on the retrieved information.

This approach reduces hallucinations and keeps responses current with your
data.

A RAG server is ideal when you have a well-defined use case with predictable query 
patterns. Consider using a RAG server when:

* users will ask predictable questions of your application about your products, 
documentation, or support knowledge base.

* you need to maintain strict control over the specific data that users can access. RAG 
defines the searchable corpus, and you define the retrieval logic.

* performance and cost are critical. A RAG system can be heavily optimised for 
specific query patterns, with caching, pre-computed embeddings, and finely-tuned 
retrieval algorithms.

* your application's queries frequently reference unstructured data like documents, 
articles, or support tickets.

## Features

- **Multiple Pipelines** - Configure separate RAG pipelines for different
  data sources, each with its own database, embedding model, and LLM.

- **Hybrid Search** - Combines vector similarity (semantic) and BM25
  (keyword) search using Reciprocal Rank Fusion for better results.

- **Multiple LLM Providers** - Support for OpenAI, Anthropic, Voyage, and
  Ollama.

- **Token Budget Management** - Automatically manages context size to
  control LLM costs.

- **Streaming Responses** - Optional real-time streaming via Server-Sent
  Events.

- **TLS Support** - Built-in HTTPS support for production deployments.

