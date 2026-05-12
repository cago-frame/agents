"""Abstract base class for text chunkers."""

from __future__ import annotations

from abc import ABC, abstractmethod
from dataclasses import dataclass


@dataclass
class TextChunk:
    """A chunk of text with positional info."""

    content: str
    index: int
    start_char: int = 0
    end_char: int = 0


class TextChunker(ABC):
    """Abstract interface for splitting text into chunks."""

    @abstractmethod
    def chunk(self, text: str) -> list[TextChunk]:
        """Split text into chunks.

        Args:
            text: Full text to split.

        Returns:
            List of TextChunk objects.
        """
