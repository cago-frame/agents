"""MIME-type to parser registry."""

from __future__ import annotations

import logging
from typing import TYPE_CHECKING

from rag_service.document.parser.base import DocumentParser
from rag_service.document.parser.docx_parser import DocxParser
from rag_service.document.parser.html_parser import HTMLParser
from rag_service.document.parser.mdx_parser import MDXParser
from rag_service.document.parser.pdf_parser import PDFParser
from rag_service.document.parser.text_parser import TextParser
from rag_service.utils.errors import UnsupportedFileTypeError

if TYPE_CHECKING:
    pass

logger = logging.getLogger(__name__)


class ParserRegistry:
    """Registry mapping MIME types to document parsers."""

    def __init__(self) -> None:
        self._parsers: dict[str, DocumentParser] = {}

    def register(self, parser: DocumentParser) -> None:
        """Register a parser for its supported MIME types."""
        for mime_type in parser.supported_mime_types():
            self._parsers[mime_type] = parser
            logger.debug("Registered parser for %s: %s", mime_type, type(parser).__name__)

    def get_parser(self, mime_type: str) -> DocumentParser:
        """Get the parser for a MIME type.

        Raises UnsupportedFileTypeError if no parser is registered.
        """
        parser = self._parsers.get(mime_type)
        if parser is None:
            raise UnsupportedFileTypeError(mime_type)
        return parser

    def supported_types(self) -> list[str]:
        """Return all supported MIME types."""
        return list(self._parsers.keys())


def create_default_registry() -> ParserRegistry:
    """Create a registry with all built-in parsers."""
    registry = ParserRegistry()
    registry.register(TextParser())
    registry.register(PDFParser())
    registry.register(DocxParser())
    registry.register(HTMLParser())
    registry.register(MDXParser())
    return registry
