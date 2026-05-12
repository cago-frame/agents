"""HTML document parser using BeautifulSoup."""

from __future__ import annotations

import logging

from bs4 import BeautifulSoup

from rag_service.document.parser.base import DocumentParser, ParsedDocument
from rag_service.utils.errors import DocumentParseError

logger = logging.getLogger(__name__)


class HTMLParser(DocumentParser):
    """Parser for HTML files using BeautifulSoup."""

    def supported_mime_types(self) -> list[str]:
        return [
            "text/html",
            "application/xhtml+xml",
        ]

    async def parse(self, data: bytes, filename: str) -> ParsedDocument:
        try:
            html_text = data.decode("utf-8")
        except UnicodeDecodeError:
            html_text = data.decode("latin-1")

        try:
            soup = BeautifulSoup(html_text, "html.parser")
        except Exception as e:
            raise DocumentParseError(f"Failed to parse HTML '{filename}': {e}") from e

        # Remove script and style elements
        for element in soup(["script", "style", "nav", "footer", "header"]):
            element.decompose()

        # Extract title
        title = ""
        title_tag = soup.find("title")
        if title_tag:
            title = title_tag.get_text(strip=True)

        # Get text content
        text = soup.get_text(separator="\n", strip=True)

        # Clean up multiple blank lines
        lines = [line.strip() for line in text.splitlines()]
        content = "\n".join(line for line in lines if line)

        if not content:
            raise DocumentParseError(f"HTML '{filename}' contains no extractable text")

        return ParsedDocument(
            content=content,
            metadata={"filename": filename, "parser": "html"},
            title=title or filename,
        )
