"""Plain text document parser."""

from __future__ import annotations

from rag_service.document.parser.base import DocumentParser, ParsedDocument


class TextParser(DocumentParser):
    """Parser for plain text files."""

    def supported_mime_types(self) -> list[str]:
        return [
            "text/plain",
            "text/csv",
            "text/markdown",
            "text/x-markdown",
        ]

    async def parse(self, data: bytes, filename: str) -> ParsedDocument:
        # Try UTF-8, fall back to latin-1
        try:
            content = data.decode("utf-8")
        except UnicodeDecodeError:
            content = data.decode("latin-1")

        return ParsedDocument(
            content=content,
            metadata={"filename": filename, "parser": "text"},
            title=filename,
        )
