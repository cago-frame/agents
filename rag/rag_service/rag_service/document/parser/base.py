"""Abstract base class for document parsers."""

from __future__ import annotations

from abc import ABC, abstractmethod
from dataclasses import dataclass, field
from typing import Any


@dataclass
class ParsedDocument:
    """Result of parsing a document."""

    content: str
    metadata: dict[str, Any] = field(default_factory=dict)
    title: str = ""
    pages: int = 0


class DocumentParser(ABC):
    """Abstract interface for document parsers."""

    @abstractmethod
    def supported_mime_types(self) -> list[str]:
        """Return the MIME types this parser supports."""

    @abstractmethod
    async def parse(self, data: bytes, filename: str) -> ParsedDocument:
        """Parse raw file data into a ParsedDocument.

        Args:
            data: Raw file bytes.
            filename: Original filename (for metadata).

        Returns:
            ParsedDocument with extracted text and metadata.
        """
