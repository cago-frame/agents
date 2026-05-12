"""FastAPI dependency injection for shared services."""

from __future__ import annotations

from fastapi import Request

from rag_service.config import Settings
from rag_service.embedding.base import EmbeddingProvider
from rag_service.vectorstore.base import VectorStore


def get_settings(request: Request) -> Settings:
    """Get application settings."""
    return request.app.state.settings


def get_embedding_provider(request: Request) -> EmbeddingProvider:
    """Get the configured embedding provider."""
    return request.app.state.embedding_provider


def get_vector_store(request: Request) -> VectorStore:
    """Get the configured vector store."""
    return request.app.state.vector_store
