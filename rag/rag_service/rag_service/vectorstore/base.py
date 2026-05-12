"""Abstract base class for vector store backends."""

from __future__ import annotations

from abc import ABC, abstractmethod
from dataclasses import dataclass, field
from typing import Any


@dataclass
class SearchResult:
    """A single search result from the vector store."""

    document_id: str
    score: float
    content: str
    metadata: dict[str, Any] = field(default_factory=dict)
    vector: list[float] | None = None


class VectorStore(ABC):
    """Abstract interface for vector store operations."""

    # --- Index management ---

    @abstractmethod
    async def create_index(
        self,
        index_name: str,
        dimension: int,
        similarity: str = "cosine",
    ) -> None:
        """Create a new vector index."""

    @abstractmethod
    async def delete_index(self, index_name: str) -> None:
        """Delete an existing index."""

    @abstractmethod
    async def list_indices(self) -> list[dict[str, Any]]:
        """List all managed vector indices."""

    @abstractmethod
    async def index_exists(self, index_name: str) -> bool:
        """Check if an index exists."""

    # --- Document CRUD ---

    @abstractmethod
    async def insert(
        self,
        index_name: str,
        document_id: str,
        content: str,
        vector: list[float],
        metadata: dict[str, Any] | None = None,
    ) -> str:
        """Insert a single document. Returns the document ID."""

    @abstractmethod
    async def batch_insert(
        self,
        index_name: str,
        documents: list[dict[str, Any]],
    ) -> list[str]:
        """Insert multiple documents. Returns list of document IDs."""

    @abstractmethod
    async def delete(self, index_name: str, document_id: str) -> None:
        """Delete a single document by ID."""

    @abstractmethod
    async def delete_by_metadata(
        self,
        index_name: str,
        filters: dict[str, Any],
    ) -> int:
        """Delete documents matching metadata filters. Returns count of deleted documents."""

    # --- Search ---

    @abstractmethod
    async def search(
        self,
        index_name: str,
        query_vector: list[float],
        top_k: int = 10,
        min_score: float = 0.0,
        filters: dict[str, Any] | None = None,
    ) -> list[SearchResult]:
        """Perform vector similarity search."""

    # --- Lifecycle ---

    async def close(self) -> None:
        """Close any open connections. Override in implementations."""
