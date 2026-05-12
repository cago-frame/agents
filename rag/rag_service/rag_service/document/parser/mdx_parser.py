"""MDX document parser.

MDX is Markdown + JSX. This parser strips JSX/import/export content
and parses the remaining Markdown, optionally extracting frontmatter metadata.
"""

from __future__ import annotations

import re

from rag_service.document.parser.base import DocumentParser, ParsedDocument

# Pattern to match YAML frontmatter block (--- ... ---)
_FRONTMATTER_RE = re.compile(r"\A---\s*\n(.*?\n)---\s*\n", re.DOTALL)

# Patterns to strip from MDX content
# import statements: import ... from '...'; or import '...';
_IMPORT_RE = re.compile(r"^import\s+.+$", re.MULTILINE)
# export statements: export default ..., export const ..., etc.
_EXPORT_RE = re.compile(r"^export\s+.+$", re.MULTILINE)
# JSX self-closing tags: <Component foo="bar" />
_JSX_SELF_CLOSING_RE = re.compile(r"<[A-Z][A-Za-z0-9.]*\b[^>]*/\s*>")
# JSX block-level component tags (opening + content + closing)
# Matches <Component ...>...</Component> including multiline
_JSX_BLOCK_RE = re.compile(
    r"<([A-Z][A-Za-z0-9.]*)\b[^>]*>[\s\S]*?</\1\s*>",
)
# JSX opening tags without closing (standalone): <Component ... >
_JSX_OPEN_TAG_RE = re.compile(r"<[A-Z][A-Za-z0-9.]*\b[^>]*>")
# JSX closing tags: </Component>
_JSX_CLOSE_TAG_RE = re.compile(r"</[A-Z][A-Za-z0-9.]*\s*>")


def _extract_frontmatter(text: str) -> tuple[dict[str, str], str]:
    """Extract YAML frontmatter and return (metadata, remaining_text).

    We do a lightweight key: value parse instead of requiring PyYAML.
    """
    match = _FRONTMATTER_RE.match(text)
    if not match:
        return {}, text

    raw = match.group(1)
    metadata: dict[str, str] = {}
    for line in raw.splitlines():
        line = line.strip()
        if ":" in line:
            key, _, value = line.partition(":")
            key = key.strip()
            value = value.strip().strip("\"'")
            if key:
                metadata[key] = value

    remaining = text[match.end() :]
    return metadata, remaining


def _clean_mdx(text: str) -> str:
    """Remove JSX / import / export artefacts from MDX source."""
    text = _IMPORT_RE.sub("", text)
    text = _EXPORT_RE.sub("", text)
    text = _JSX_SELF_CLOSING_RE.sub("", text)
    text = _JSX_BLOCK_RE.sub("", text)
    text = _JSX_OPEN_TAG_RE.sub("", text)
    text = _JSX_CLOSE_TAG_RE.sub("", text)

    # Collapse runs of blank lines into at most two newlines
    text = re.sub(r"\n{3,}", "\n\n", text)
    return text.strip()


class MDXParser(DocumentParser):
    """Parser for MDX (Markdown + JSX) files."""

    def supported_mime_types(self) -> list[str]:
        return ["text/mdx"]

    async def parse(self, data: bytes, filename: str) -> ParsedDocument:
        # Decode
        try:
            text = data.decode("utf-8")
        except UnicodeDecodeError:
            text = data.decode("latin-1")

        # Extract frontmatter
        frontmatter, body = _extract_frontmatter(text)

        # Clean JSX artefacts
        content = _clean_mdx(body)

        # Build metadata
        metadata: dict[str, str] = {"filename": filename, "parser": "mdx"}
        metadata.update(frontmatter)

        title = frontmatter.get("title", filename)

        return ParsedDocument(
            content=content,
            metadata=metadata,
            title=title,
        )
