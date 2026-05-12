"""Scan command: discover local files and process them through the RAG pipeline."""

from __future__ import annotations

import asyncio
import fnmatch
import logging
import mimetypes
import os
import sys
import time
from dataclasses import dataclass, field
from pathlib import Path
from typing import Any

from rag_service.config import Settings, load_settings
from rag_service.document.parser.registry import create_default_registry
from rag_service.document.pipeline import DocumentPipeline
from rag_service.embedding.factory import create_embedding_provider
from rag_service.vectorstore.factory import create_vector_store

logger = logging.getLogger(__name__)

# Directories to always skip
SKIP_DIRS = {
    ".git",
    ".svn",
    ".hg",
    "__pycache__",
    "node_modules",
    ".venv",
    "venv",
    ".tox",
    ".mypy_cache",
    ".pytest_cache",
    ".ruff_cache",
    ".eggs",
    "dist",
    "build",
    ".chroma_data",
}

# Extra MIME type mappings for common file types
EXTRA_MIME_TYPES = {
    ".md": "text/markdown",
    ".mdx": "text/mdx",
    ".markdown": "text/markdown",
    ".rst": "text/plain",
    ".txt": "text/plain",
    ".csv": "text/csv",
    ".log": "text/plain",
    ".json": "text/plain",
    ".yaml": "text/plain",
    ".yml": "text/plain",
    ".toml": "text/plain",
    ".ini": "text/plain",
    ".cfg": "text/plain",
    ".conf": "text/plain",
    ".xml": "text/plain",
    ".html": "text/html",
    ".htm": "text/html",
    ".xhtml": "application/xhtml+xml",
    ".pdf": "application/pdf",
    ".docx": "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
    ".py": "text/plain",
    ".js": "text/plain",
    ".ts": "text/plain",
    ".java": "text/plain",
    ".go": "text/plain",
    ".rs": "text/plain",
    ".c": "text/plain",
    ".cpp": "text/plain",
    ".h": "text/plain",
    ".hpp": "text/plain",
    ".rb": "text/plain",
    ".php": "text/plain",
    ".sh": "text/plain",
    ".bash": "text/plain",
    ".zsh": "text/plain",
    ".sql": "text/plain",
    ".r": "text/plain",
    ".R": "text/plain",
    ".swift": "text/plain",
    ".kt": "text/plain",
    ".scala": "text/plain",
    ".lua": "text/plain",
}


@dataclass
class FileInfo:
    """Information about a discovered file."""

    path: Path
    mime_type: str
    size: int


@dataclass
class ScanProgress:
    """Track scanning progress and statistics."""

    total_files: int = 0
    processed: int = 0
    succeeded: int = 0
    failed: int = 0
    skipped: int = 0
    errors: list[dict[str, str]] = field(default_factory=list)
    start_time: float = 0.0

    def start(self, total: int) -> None:
        self.total_files = total
        self.start_time = time.time()

    def record_success(self, filename: str) -> None:
        self.processed += 1
        self.succeeded += 1
        self._print_progress(filename, "OK")

    def record_failure(self, filename: str, error: str) -> None:
        self.processed += 1
        self.failed += 1
        self.errors.append({"file": filename, "error": error})
        self._print_progress(filename, "FAIL")

    def record_skip(self, filename: str, reason: str) -> None:
        self.processed += 1
        self.skipped += 1
        self._print_progress(filename, f"SKIP: {reason}")

    def _print_progress(self, filename: str, status: str) -> None:
        pct = (self.processed / self.total_files * 100) if self.total_files > 0 else 0
        sys.stderr.write(
            f"\r[{self.processed}/{self.total_files}] ({pct:.0f}%) {status}: {filename}"
        )
        sys.stderr.write("\033[K\n")  # Clear to end of line
        sys.stderr.flush()

    def print_summary(self) -> None:
        elapsed = time.time() - self.start_time
        print("\n" + "=" * 60)
        print("Scan Summary")
        print("=" * 60)
        print(f"  Total files discovered: {self.total_files}")
        print(f"  Succeeded:              {self.succeeded}")
        print(f"  Failed:                 {self.failed}")
        print(f"  Skipped:                {self.skipped}")
        print(f"  Time elapsed:           {elapsed:.1f}s")
        if self.errors:
            print("\nErrors:")
            for err in self.errors[:20]:
                print(f"  - {err['file']}: {err['error']}")
            if len(self.errors) > 20:
                print(f"  ... and {len(self.errors) - 20} more errors")
        print("=" * 60)


def detect_mime_type(filepath: Path) -> str | None:
    """Detect MIME type for a file using extension mapping and mimetypes module."""
    ext = filepath.suffix.lower()

    # Check our custom mapping first
    if ext in EXTRA_MIME_TYPES:
        return EXTRA_MIME_TYPES[ext]

    # Fall back to mimetypes module
    mime, _ = mimetypes.guess_type(str(filepath))
    return mime


def discover_files(
    directory: Path,
    glob_pattern: str | None = None,
    supported_types: list[str] | None = None,
) -> list[FileInfo]:
    """Recursively discover processable files in a directory.

    Args:
        directory: Root directory to scan.
        glob_pattern: Optional glob filter (e.g. "*.pdf").
        supported_types: List of supported MIME types. If None, accept all detected.

    Returns:
        List of FileInfo for files that can be processed.
    """
    files: list[FileInfo] = []

    for root, dirs, filenames in os.walk(directory):
        # Skip hidden directories and known skip dirs
        dirs[:] = [
            d
            for d in dirs
            if not d.startswith(".") and d not in SKIP_DIRS
        ]

        for name in filenames:
            # Skip hidden files
            if name.startswith("."):
                continue

            # Apply glob filter
            if glob_pattern and not fnmatch.fnmatch(name, glob_pattern):
                continue

            filepath = Path(root) / name
            mime = detect_mime_type(filepath)

            if mime is None:
                continue

            # Check against supported types if provided
            if supported_types and mime not in supported_types:
                continue

            try:
                size = filepath.stat().st_size
            except OSError:
                continue

            # Skip empty files
            if size == 0:
                continue

            files.append(FileInfo(path=filepath, mime_type=mime, size=size))

    # Sort by path for deterministic ordering
    files.sort(key=lambda f: f.path)
    return files


async def cmd_scan(args: Any) -> None:
    """Execute the scan command.

    Args:
        args: Parsed argparse namespace with scan command arguments.
    """
    directory = Path(args.directory).resolve()

    if not directory.is_dir():
        print(f"Error: '{directory}' is not a valid directory.", file=sys.stderr)
        sys.exit(1)

    # Load configuration
    config_path = getattr(args, "config", None)
    settings = load_settings(config_path)

    # Apply CLI overrides
    if args.vectorstore_provider:
        settings.vectorstore.provider = args.vectorstore_provider
    if args.chroma_dir:
        settings.vectorstore.chroma.persist_directory = args.chroma_dir

    # Setup logging
    logging.basicConfig(
        level=logging.INFO,
        format="%(asctime)s - %(name)s - %(levelname)s - %(message)s",
    )

    # Get supported MIME types from parser registry
    registry = create_default_registry()
    supported_types = registry.supported_types()

    print(f"Scanning directory: {directory}")
    print(f"Vectorstore provider: {settings.vectorstore.provider}")
    print(f"Index name: {args.index_name}")
    print(f"Supported types: {', '.join(supported_types)}")
    if args.glob:
        print(f"Glob filter: {args.glob}")
    print()

    # Discover files
    files = discover_files(
        directory=directory,
        glob_pattern=args.glob,
        supported_types=supported_types,
    )

    if not files:
        print("No supported files found in the directory.")
        return

    total_size = sum(f.size for f in files)
    print(f"Found {len(files)} files ({_format_size(total_size)})")
    print()

    # Dry run: just list files and exit
    if args.dry_run:
        print("Dry run - files that would be processed:")
        print("-" * 60)
        for f in files:
            rel = f.path.relative_to(directory) if f.path.is_relative_to(directory) else f.path
            print(f"  {rel}  ({f.mime_type}, {_format_size(f.size)})")
        print("-" * 60)
        print(f"Total: {len(files)} files ({_format_size(total_size)})")
        return

    # Initialize pipeline components
    print("Initializing embedding provider...")
    embedding_provider = create_embedding_provider(settings.embedding)
    logger.info("Embedding provider: %s", embedding_provider.model_name())

    print("Initializing vector store...")
    vector_store = await create_vector_store(settings.vectorstore)
    logger.info("Vector store: %s", settings.vectorstore.provider)

    pipeline = DocumentPipeline(
        embedding_provider=embedding_provider,
        vector_store=vector_store,
        config=settings.document,
        parser_registry=registry,
    )

    # Process files
    progress = ScanProgress()
    progress.start(len(files))

    print(f"\nProcessing {len(files)} files...\n")

    for file_info in files:
        rel_path = (
            file_info.path.relative_to(directory)
            if file_info.path.is_relative_to(directory)
            else file_info.path
        )
        filename = str(rel_path)

        try:
            data = file_info.path.read_bytes()
            result = await pipeline.process(
                data=data,
                filename=filename,
                mime_type=file_info.mime_type,
                index_name=args.index_name,
                metadata={
                    "source": "scan",
                    "source_path": str(file_info.path),
                },
            )
            if result["chunk_count"] == 0:
                progress.record_skip(filename, "no content extracted")
            else:
                progress.record_success(filename)
        except Exception as e:
            progress.record_failure(filename, str(e))
            logger.error("Failed to process %s: %s", filename, e)

    # Cleanup
    await vector_store.close()

    # Summary
    progress.print_summary()


def _format_size(size: int) -> str:
    """Format file size in human-readable form."""
    for unit in ("B", "KB", "MB", "GB"):
        if size < 1024:
            return f"{size:.1f}{unit}" if unit != "B" else f"{size}{unit}"
        size /= 1024  # type: ignore[assignment]
    return f"{size:.1f}TB"
