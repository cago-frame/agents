"""Document processing pipeline: parse → chunk → embed → store."""

from __future__ import annotations

import logging
import uuid
from typing import Any

from rag_service.config import DocumentConfig
from rag_service.document.chunker.base import TextChunker
from rag_service.document.chunker.recursive_character import RecursiveCharacterChunker
from rag_service.document.chunker.token_based import TokenBasedChunker
from rag_service.document.parser.registry import ParserRegistry, create_default_registry
from rag_service.embedding.base import EmbeddingProvider
from rag_service.vectorstore.base import VectorStore

logger = logging.getLogger(__name__)


def create_chunker(config: DocumentConfig) -> TextChunker:
    """Create a text chunker based on configuration."""
    strategy = config.chunker.strategy.lower()
    if strategy == "token_based":
        return TokenBasedChunker(
            chunk_size=config.chunker.chunk_size,
            chunk_overlap=config.chunker.chunk_overlap,
        )
    else:  # default: recursive_character
        return RecursiveCharacterChunker(
            chunk_size=config.chunker.chunk_size,
            chunk_overlap=config.chunker.chunk_overlap,
        )


class DocumentPipeline:
    """Orchestrates the full document processing flow.

    Upload file → parse text → chunk → embed → store in vector DB.
    """

    def __init__(
        self,
        embedding_provider: EmbeddingProvider,
        vector_store: VectorStore,
        config: DocumentConfig,
        parser_registry: ParserRegistry | None = None,
    ):
        self._embedding = embedding_provider
        self._store = vector_store
        self._config = config
        self._registry = parser_registry or create_default_registry()
        self._chunker = create_chunker(config)

    async def process(
        self,
        data: bytes,
        filename: str,
        mime_type: str,
        index_name: str,
        metadata: dict[str, Any] | None = None,
    ) -> dict[str, Any]:
        """Process a document through the full pipeline.

        Args:
            data: Raw file bytes.
            filename: Original filename.
            mime_type: MIME type of the file.
            index_name: Target vector store index.
            metadata: Additional metadata to store with each chunk.

        Returns:
            Dict with document_id, chunk_count, and index_name.
        """
        document_id = str(uuid.uuid4())
        extra_metadata = metadata or {}
        extra_metadata["filename"] = filename
        extra_metadata["mime_type"] = mime_type

        # 1. Parse the document
        parser = self._registry.get_parser(mime_type)
        parsed = await parser.parse(data, filename)
        logger.info("Parsed '%s': %d chars, %d pages", filename, len(parsed.content), parsed.pages)

        # 2. Chunk the text
        chunks = self._chunker.chunk(parsed.content)
        if not chunks:
            logger.warning("No chunks produced from '%s'", filename)
            return {"document_id": document_id, "chunk_count": 0, "index_name": index_name}

        logger.info("Chunked '%s' into %d chunks", filename, len(chunks))

        # 3. Generate embeddings for all chunks
        texts = [chunk.content for chunk in chunks]
        embeddings = await self._embedding.embed_texts(texts)

        # 4. Ensure index exists (auto-create if needed)
        if not await self._store.index_exists(index_name):
            await self._store.create_index(
                index_name=index_name,
                dimension=self._embedding.dimension(),
            )
            logger.info("Auto-created index '%s' with dimension %d", index_name, self._embedding.dimension())

        # 5. Batch insert into vector store
        documents = []
        for i, (chunk, embedding) in enumerate(zip(chunks, embeddings)):
            chunk_id = f"{document_id}_{i}"
            chunk_metadata = {
                **extra_metadata,
                **parsed.metadata,
                "chunk_index": i,
                "total_chunks": len(chunks),
                "document_id": document_id,
            }
            documents.append(
                {
                    "id": chunk_id,
                    "content": chunk.content,
                    "vector": embedding.vector,
                    "metadata": chunk_metadata,
                }
            )

        await self._store.batch_insert(index_name, documents)
        logger.info("Stored %d chunks for document '%s' in index '%s'", len(documents), filename, index_name)

        return {
            "document_id": document_id,
            "chunk_count": len(documents),
            "index_name": index_name,
        }
