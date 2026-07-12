-- 384 matches paraphrase-multilingual-MiniLM-L12-v2, the model fixed in
-- Phase 3. Changing the embedding model to another dimension requires a new
-- migration (and re-ingesting every document).
ALTER TABLE chunks ADD COLUMN embedding vector(384) NOT NULL;
