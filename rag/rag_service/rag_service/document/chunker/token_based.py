"""Token-based text chunker using tiktoken."""

from __future__ import annotations

import logging

import tiktoken

from rag_service.document.chunker.base import TextChunk, TextChunker

logger = logging.getLogger(__name__)


class TokenBasedChunker(TextChunker):
    """Split text based on token count using tiktoken.

    Ensures each chunk stays within a maximum token budget.
    """

    def __init__(
        self,
        chunk_size: int = 512,
        chunk_overlap: int = 64,
        encoding_name: str = "cl100k_base",
    ):
        self._chunk_size = chunk_size
        self._chunk_overlap = chunk_overlap
        try:
            self._encoding = tiktoken.get_encoding(encoding_name)
        except Exception:
            logger.warning("Failed to load encoding %s, falling back to cl100k_base", encoding_name)
            self._encoding = tiktoken.get_encoding("cl100k_base")

    def chunk(self, text: str) -> list[TextChunk]:
        if not text.strip():
            return []

        tokens = self._encoding.encode(text)
        total_tokens = len(tokens)

        if total_tokens <= self._chunk_size:
            return [
                TextChunk(
                    content=text,
                    index=0,
                    start_char=0,
                    end_char=len(text),
                )
            ]

        chunks: list[TextChunk] = []
        start = 0

        while start < total_tokens:
            end = min(start + self._chunk_size, total_tokens)
            chunk_tokens = tokens[start:end]
            chunk_text = self._encoding.decode(chunk_tokens)

            chunks.append(
                TextChunk(
                    content=chunk_text,
                    index=len(chunks),
                )
            )

            # Move forward by (chunk_size - overlap) tokens
            step = self._chunk_size - self._chunk_overlap
            if step <= 0:
                step = self._chunk_size  # Safety: avoid infinite loop
            start += step

        return chunks
