# pgEdge AI Toolkit RAG Server Quickstart

This directory contains the AI Toolkit RAG Server Quickstart demo. These files are hosted at https://downloads.pgedge.com/quickstart/rag 
to support the live demo at https://www.pgedge.com/ai-toolkit.

The purpose of this demo is to provide end users with a working interactive example of the RAG server in action with 
the fewest configuration steps. Rather than building containers from the repository, the demo pulls the latest public 
container images from the pgEdge repository.

For more detail, see the [Quick Start Guide](../../docs/quickstart_demo.md).

The vectorization process involves several steps and takes time to complete. The demo uses documentation sets that 
have been preloaded into the database and vectorized before being captured with `pg_dump`. The *docker-compose.yml* 
file downloads the content and restores it into the demo database in the user's Docker environment. Downloading the 
preprocessed data is faster and easier than automating its creation from scratch as part of the demo. The demo directs 
users to the RAG blog series, which outlines the complete process in detail. For future maintenance, here is how the 
database dump for the demo was created.

```shell
# Download the repositories
git clone git@github.com:pgEdge/pgedge-docloader.git
git clone git@github.com:pgEdge/pgedge-vectorizer.git

# Store your OpenAI API key securely
mkdir -p ~/.pgedge-vectorizer
echo "sk-proj-YOUR_KEY_HERE" > ~/.pgedge-vectorizer/openai-api-key
chmod 600 ~/.pgedge-vectorizer/openai-api-key

# Download the two documentation sets
mkdir /tmp/docs
cd /tmp/docs
git clone --depth 1 -b REL_17_STABLE https://github.com/postgres/postgres.git
git clone --depth 1 git@github.com:pgEdge/pgedge-docs.git

# Launch pgedge-postgres image (which contains the vectorizer extension)
docker run -d \
    --name ragdb-builder \
    -e POSTGRES_USER=docuser \
    -e POSTGRES_PASSWORD=docpass \
    -e POSTGRES_DB=ragdb \
    -p 5433:5432 \
    -v ~/.pgedge-vectorizer/openai-api-key:/var/lib/pgsql/openai-api-key:ro \
    ghcr.io/pgedge/pgedge-postgres:17-spock5-standard \
    postgres \
      -c listen_addresses='*' \
      -c shared_preload_libraries='pgedge_vectorizer' \
      -c pgedge_vectorizer.provider='openai' \
      -c pgedge_vectorizer.api_key_file='/var/lib/pgsql/openai-api-key' \
      -c pgedge_vectorizer.model='text-embedding-3-small' \
      -c pgedge_vectorizer.databases='ragdb' \
      -c pgedge_vectorizer.num_workers=2 \
      -c pgedge_vectorizer.batch_size=10 \
      -c pgedge_vectorizer.auto_cleanup_hours=0

# Log into the database and enable extensions and set up required tables
docker exec -i ragdb-builder psql -U docuser -d ragdb << 'EOSQL'
 -- Enable required extensions
 CREATE EXTENSION IF NOT EXISTS vector;
 CREATE EXTENSION IF NOT EXISTS pgedge_vectorizer;

 -- Create documents table
 CREATE TABLE documents (
     id SERIAL PRIMARY KEY,
     title TEXT,
     content TEXT NOT NULL,
     source BYTEA,
     filename TEXT UNIQUE NOT NULL,
     product TEXT,
     version TEXT,
     file_modified TIMESTAMPTZ,
     created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
     updated_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
 );

 -- Create indexes
 CREATE INDEX idx_documents_filename ON documents(filename);
 CREATE INDEX idx_documents_product ON documents(product);
 CREATE INDEX idx_documents_content_fts ON documents
     USING gin(to_tsvector('english', content));
EOSQL

# Build the docloader
cd ~/work/ws/pgedge-docloader
make build
DOCLOADER=/path/to/pgedge-docloader/bin/pgedge-docloader

# Load the PostgreSQL 17 documentation set
cd /tmp
$DOCLOADER \
   --source "./docs/postgres/doc/src/sgml/**/*.sgml" \
   --db-host localhost \
   --db-port 5433 \
   --db-name ragdb \
   --db-user docuser \
   --db-table documents \
   --col-doc-title title \
   --col-doc-content content \
   --col-source-content source \
   --col-file-name filename \
   --col-file-modified file_modified \
   --set-column "product=PostgreSQL" \
   --set-column "version=17"

# Load the pgEdge documentation set
$DOCLOADER \
   --source "./docs/pgedge-docs/docs/platform/**/*.md" \
   --db-host localhost \
   --db-port 5433 \
   --db-name ragdb \
   --db-user docuser \
   --db-table documents \
   --col-doc-title title \
   --col-doc-content content \
   --col-source-content source \
   --col-file-name filename \
   --col-file-modified file_modified \
   --set-column "product=pgEdge Platform"

# Enable vectorization on documents table
docker exec -i ragdb-builder psql -U docuser -d ragdb << 'EOSQL'
 SELECT pgedge_vectorizer.enable_vectorization(
     source_table := 'documents',
     source_column := 'content',
     chunk_strategy := 'token_based',
     chunk_size := 400,
     chunk_overlap := 50,
     embedding_dimension := 1536  -- text-embedding-3-small uses 1536 dimensions
 );

 -- Verify chunk table was created
 \dt documents_content_chunks

 -- Check initial queue status
 SELECT * FROM pgedge_vectorizer.queue_status;
EOSQL

# Monitor the vectorization process (may take a minute before showing progress)
while true; do
   docker exec ragdb-builder psql -U docuser -d ragdb -t -c "
     SELECT
       'Pending: ' || COUNT(*) FILTER (WHERE status = 'pending') ||
       ', Processing: ' || COUNT(*) FILTER (WHERE status = 'processing') ||
       ', Completed: ' || COUNT(*) FILTER (WHERE status = 'completed') ||
       ', Failed: ' || COUNT(*) FILTER (WHERE status = 'failed')
     FROM pgedge_vectorizer.queue;
   "
   sleep 10
done

# Vectorization took five minutes to vectorize 6,300 documents in the test environment

# Dump the contents of the database
PGPASSWORD=docpass pg_dump -h localhost -p 5433 -U docuser ragdb \
    --schema=public \
    --exclude-schema='pgedge_vectorizer' \
    --no-owner \
    --no-privileges \
    | gzip > ragdb-postgres-pgedge.sql.gz

# Restore to a second database to test if the RAG demo can be built on the restored content
docker run -d \
    --name ragdb-test \
    -e POSTGRES_USER=docuser \
    -e POSTGRES_PASSWORD=docpass \
    -e POSTGRES_DB=ragdb \
    -p 5435:5432 \
    -v ~/.pgedge-vectorizer/openai-api-key:/var/lib/pgsql/openai-api-key:ro \
    ghcr.io/pgedge/pgedge-postgres:17-spock5-standard \
    postgres \
      -c listen_addresses='*' \
      -c shared_preload_libraries='pgedge_vectorizer' \
      -c pgedge_vectorizer.provider='openai' \
      -c pgedge_vectorizer.api_key_file='/var/lib/pgsql/openai-api-key' \
      -c pgedge_vectorizer.model='text-embedding-3-small' \
      -c pgedge_vectorizer.databases='ragdb' \
      -c pgedge_vectorizer.num_workers=2 \
      -c pgedge_vectorizer.batch_size=10 \
      -c pgedge_vectorizer.auto_cleanup_hours=0

docker exec ragdb-test psql -U docuser -d ragdb -c "CREATE EXTENSION IF NOT EXISTS vector; CREATE EXTENSION IF NOT EXISTS pgedge_vectorizer;"

gunzip -c /tmp/ragdb-postgres-pgedge.sql.gz | \
grep -v "pgedge_vectorizer\|CREATE SCHEMA public" | \
docker exec -i ragdb-test psql -U docuser -d ragdb
```

The *ragdb-postgres-pgedge.sql.gz* artifact is hosted in Amazon S3 on the public download server so *docker-compose.yml* can load it into the demo database in the user's Docker environment.
