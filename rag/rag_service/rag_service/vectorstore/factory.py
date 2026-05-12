"""Factory for creating vector store backends based on configuration."""

from __future__ import annotations

from rag_service.config import VectorStoreConfig
from rag_service.utils.errors import ProviderNotConfiguredError
from rag_service.vectorstore.base import VectorStore


async def create_vector_store(config: VectorStoreConfig) -> VectorStore:
    """Create a vector store instance based on the configuration."""
    provider = config.provider.lower()

    if provider == "elasticsearch":
        from rag_service.vectorstore.elasticsearch_store import ElasticsearchVectorStore

        store = ElasticsearchVectorStore(config.elasticsearch)
        await store.initialize()
        return store

    elif provider == "chroma":
        try:
            from rag_service.vectorstore.chroma_store import ChromaVectorStore

            store = ChromaVectorStore(config.chroma)
            await store.initialize()
            return store
        except ImportError:
            raise ProviderNotConfiguredError(
                "chroma",
                "ChromaDB is not installed. Install with: pip install rag-service[local] "
                "or: pip install chromadb>=0.5.0",
            )

    else:
        raise ProviderNotConfiguredError(provider, f"Unknown vector store provider: {provider}")
