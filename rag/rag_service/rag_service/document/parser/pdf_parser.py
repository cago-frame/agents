"""PDF document parser using PyMuPDF."""

from __future__ import annotations

import asyncio
import logging
from functools import partial

import pymupdf

from rag_service.document.parser.base import DocumentParser, ParsedDocument
from rag_service.utils.errors import DocumentParseError

logger = logging.getLogger(__name__)


class PDFParser(DocumentParser):
    """Parser for PDF files using PyMuPDF (fitz)."""

    def supported_mime_types(self) -> list[str]:
        return ["application/pdf"]

    async def parse(self, data: bytes, filename: str) -> ParsedDocument:
        loop = asyncio.get_event_loop()
        return await loop.run_in_executor(None, partial(self._parse_sync, data, filename))

    def _parse_sync(self, data: bytes, filename: str) -> ParsedDocument:
        try:
            doc = pymupdf.open(stream=data, filetype="pdf")
        except Exception as e:
            raise DocumentParseError(f"Failed to open PDF '{filename}': {e}") from e

        pages_text: list[str] = []
        try:
            for page in doc:
                text = page.get_text()
                if text.strip():
                    pages_text.append(text)
        finally:
            doc.close()

        if not pages_text:
            raise DocumentParseError(f"PDF '{filename}' contains no extractable text")

        content = "\n\n".join(pages_text)
        return ParsedDocument(
            content=content,
            metadata={"filename": filename, "parser": "pdf"},
            title=filename,
            pages=len(pages_text),
        )
