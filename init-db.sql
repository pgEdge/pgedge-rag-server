-- Initialize database for pgEdge RAG Server
-- This script creates the necessary extensions and a sample table structure

-- Enable pgvector extension
CREATE EXTENSION IF NOT EXISTS vector;

-- Create a sample documents table
-- Adjust the vector dimension based on your embedding model:
-- - OpenAI text-embedding-3-small: 1536 dimensions
-- - OpenAI text-embedding-3-large: 3072 dimensions
-- - Voyage AI models: typically 1024 or 1536 dimensions
CREATE TABLE IF NOT EXISTS documents (
    id SERIAL PRIMARY KEY,
    content TEXT NOT NULL,
    embedding vector(1536),
    title TEXT,
    source TEXT,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- Create an index for vector similarity search (using HNSW algorithm)
CREATE INDEX IF NOT EXISTS documents_embedding_idx
ON documents USING hnsw (embedding vector_cosine_ops);

-- Create a GIN index for BM25 text search (used in hybrid search)
CREATE INDEX IF NOT EXISTS documents_content_idx
ON documents USING gin (to_tsvector('english', content));

-- Optional: Insert sample data
-- INSERT INTO documents (content, title, source) VALUES
-- ('Sample document content here', 'Sample Document', 'manual');

-- Grant permissions to the postgres user (adjust if using different user)
GRANT ALL PRIVILEGES ON TABLE documents TO postgres;
GRANT USAGE, SELECT ON SEQUENCE documents_id_seq TO postgres;
