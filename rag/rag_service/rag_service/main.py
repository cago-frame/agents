"""FastAPI application entry point with lifespan management."""

from __future__ import annotations

import argparse
import asyncio
import logging
import sys
from contextlib import asynccontextmanager
from typing import AsyncIterator

import uvicorn
from fastapi import FastAPI

from rag_service.config import Settings, load_settings
from rag_service.utils.errors import register_error_handlers

logger = logging.getLogger(__name__)


@asynccontextmanager
async def lifespan(app: FastAPI) -> AsyncIterator[None]:
    """Manage application startup and shutdown lifecycle."""
    settings: Settings = app.state.settings

    # Initialize embedding provider
    from rag_service.embedding.factory import create_embedding_provider

    provider = create_embedding_provider(settings.embedding)
    app.state.embedding_provider = provider
    logger.info("Embedding provider initialized: %s", provider.model_name)

    # Initialize vector store
    from rag_service.vectorstore.factory import create_vector_store

    store = await create_vector_store(settings.vectorstore)
    app.state.vector_store = store
    logger.info("Vector store initialized: %s", settings.vectorstore.provider)

    yield

    # Shutdown: close connections
    if hasattr(app.state, "vector_store") and app.state.vector_store is not None:
        await app.state.vector_store.close()
        logger.info("Vector store connection closed")


def create_app(settings: Settings | None = None) -> FastAPI:
    """Create and configure the FastAPI application."""
    if settings is None:
        settings = load_settings()

    logging.basicConfig(
        level=getattr(logging, settings.server.log_level.upper(), logging.INFO),
        format="%(asctime)s - %(name)s - %(levelname)s - %(message)s",
    )

    app = FastAPI(
        title="Cago RAG Service",
        description="RAG microservice providing embeddings, document parsing, and vector search",
        version="0.1.0",
        lifespan=lifespan,
    )
    app.state.settings = settings

    # Register error handlers
    register_error_handlers(app)

    # Health check
    @app.get("/healthz", tags=["health"])
    async def healthz():
        return {"status": "ok"}

    # Register API routes
    from rag_service.api.router import api_router

    app.include_router(api_router)

    return app


def _build_parser() -> argparse.ArgumentParser:
    """Build the CLI argument parser with subcommands."""
    parser = argparse.ArgumentParser(
        prog="rag-service",
        description="Cago RAG Service - embeddings, document parsing, and vector search",
    )
    subparsers = parser.add_subparsers(dest="command")

    # --- serve subcommand ---
    serve_parser = subparsers.add_parser(
        "serve",
        help="Start the HTTP API server (default)",
    )
    serve_parser.add_argument(
        "--config",
        type=str,
        default=None,
        help="Path to config YAML file",
    )

    # --- scan subcommand ---
    scan_parser = subparsers.add_parser(
        "scan",
        help="Scan a local directory and ingest files into the vector store",
    )
    scan_parser.add_argument(
        "directory",
        type=str,
        help="Directory path to scan",
    )
    scan_parser.add_argument(
        "--index-name",
        type=str,
        default="documents",
        help="Target index/collection name (default: documents)",
    )
    scan_parser.add_argument(
        "--vectorstore-provider",
        type=str,
        default=None,
        help="Override vectorstore provider (elasticsearch|chroma)",
    )
    scan_parser.add_argument(
        "--chroma-dir",
        type=str,
        default=None,
        help="ChromaDB persist directory (overrides config)",
    )
    scan_parser.add_argument(
        "--glob",
        type=str,
        default=None,
        help="File filter glob pattern (e.g. '*.pdf')",
    )
    scan_parser.add_argument(
        "--dry-run",
        action="store_true",
        default=False,
        help="List discovered files without processing",
    )
    scan_parser.add_argument(
        "--config",
        type=str,
        default=None,
        help="Path to config YAML file",
    )

    return parser


def _cmd_serve(args: argparse.Namespace) -> None:
    """Run the HTTP API server."""
    config_path = getattr(args, "config", None)
    settings = load_settings(config_path)
    app = create_app(settings)
    uvicorn.run(
        app,
        host=settings.server.host,
        port=settings.server.port,
        log_level=settings.server.log_level,
    )


def cli() -> None:
    """CLI entry point for running the service."""
    parser = _build_parser()
    args = parser.parse_args()

    # Default to serve if no subcommand given
    if args.command is None or args.command == "serve":
        _cmd_serve(args)
    elif args.command == "scan":
        from rag_service.cli.scan import cmd_scan

        asyncio.run(cmd_scan(args))
    else:
        parser.print_help()
        sys.exit(1)


if __name__ == "__main__":
    cli()
