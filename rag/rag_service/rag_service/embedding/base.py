"""Abstract base class for embedding providers."""

from __future__ import annotations

from abc import ABC, abstractmethod
from dataclasses import dataclass


@dataclass
class EmbeddingResult:
    """Result of embedding a single text."""

    vector: list[float]
    token_count: int = 0


class EmbeddingProvider(ABC):
    """Abstract interface for text embedding providers."""

    @abstractmethod
    async def embed_texts(self, texts: list[str]) -> list[EmbeddingResult]:
        """Generate embeddings for a list of texts.

        Args:
            texts: List of text strings to embed.

        Returns:
            List of EmbeddingResult, one per input text.
        """

    @abstractmethod
    def dimension(self) -> int:
        """Return the dimensionality of the embedding vectors."""

    @abstractmethod
    def model_name(self) -> str:
        """Return the name/identifier of the embedding model."""
