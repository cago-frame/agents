"""Recursive character-based text chunker."""

from __future__ import annotations

from rag_service.document.chunker.base import TextChunk, TextChunker


class RecursiveCharacterChunker(TextChunker):
    """Split text recursively using a hierarchy of separators.

    Tries to split on paragraph boundaries first, then sentences,
    then words, and finally characters.
    """

    def __init__(
        self,
        chunk_size: int = 512,
        chunk_overlap: int = 64,
        separators: list[str] | None = None,
    ):
        self._chunk_size = chunk_size
        self._chunk_overlap = chunk_overlap
        self._separators = separators or ["\n\n", "\n", ". ", " ", ""]

    def chunk(self, text: str) -> list[TextChunk]:
        if not text.strip():
            return []

        chunks = self._split_recursive(text, self._separators)
        return self._merge_chunks(chunks)

    def _split_recursive(self, text: str, separators: list[str]) -> list[str]:
        """Recursively split text using the separator hierarchy."""
        if len(text) <= self._chunk_size:
            return [text] if text.strip() else []

        # Find the best separator for this level
        separator = separators[-1]  # Fallback to last (finest) separator
        for sep in separators:
            if sep == "":
                separator = sep
                break
            if sep in text:
                separator = sep
                break

        # Split using the chosen separator
        if separator:
            parts = text.split(separator)
        else:
            # Character-level split
            parts = list(text)

        # Merge parts into chunks that fit within chunk_size
        result: list[str] = []
        current = ""

        remaining_separators = separators[separators.index(separator) + 1 :] if separator in separators else separators

        for part in parts:
            candidate = current + separator + part if current else part

            if len(candidate) <= self._chunk_size:
                current = candidate
            else:
                if current:
                    result.append(current)
                # If a single part is too large, split it further
                if len(part) > self._chunk_size and remaining_separators:
                    sub_chunks = self._split_recursive(part, remaining_separators)
                    result.extend(sub_chunks)
                    current = ""
                else:
                    current = part

        if current and current.strip():
            result.append(current)

        return result

    def _merge_chunks(self, raw_chunks: list[str]) -> list[TextChunk]:
        """Create TextChunk objects with overlap."""
        if not raw_chunks:
            return []

        chunks: list[TextChunk] = []
        char_offset = 0

        for i, text in enumerate(raw_chunks):
            content = text.strip()
            if not content:
                continue

            # Add overlap from previous chunk
            if i > 0 and self._chunk_overlap > 0 and chunks:
                prev_content = chunks[-1].content
                overlap_text = prev_content[-self._chunk_overlap :]
                # Only prepend overlap if it doesn't make chunk too large
                if len(overlap_text) + len(content) <= self._chunk_size * 1.5:
                    content = overlap_text + " " + content

            chunk = TextChunk(
                content=content,
                index=len(chunks),
                start_char=char_offset,
                end_char=char_offset + len(text),
            )
            chunks.append(chunk)
            char_offset += len(text)

        return chunks
