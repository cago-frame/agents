"""Documents API endpoints: upload and delete documents."""

from __future__ import annotations

import json
import logging
import mimetypes
from typing import Any

from fastapi import APIRouter, Depends, File, Form, UploadFile

from rag_service.api.v1.schemas.documents import (
    DeleteDocumentRequest,
    DeleteDocumentResponse,
    DocumentUploadResponse,
)
from rag_service.config import Settings
from rag_service.dependencies import get_embedding_provider, get_settings, get_vector_store
from rag_service.document.pipeline import DocumentPipeline
from rag_service.embedding.base import EmbeddingProvider
from rag_service.vectorstore.base import VectorStore

logger = logging.getLogger(__name__)

router = APIRouter()


@router.post("/documents", response_model=DocumentUploadResponse, status_code=201)
async def upload_document(
    file: UploadFile = File(...),
    index_name: str = Form(...),
    metadata: str = Form(default="{}"),
    settings: Settings = Depends(get_settings),
    provider: EmbeddingProvider = Depends(get_embedding_provider),
    store: VectorStore = Depends(get_vector_store),
) -> DocumentUploadResponse:
    """Upload a document for processing: parse, chunk, embed, and store.

    - **file**: The document file (PDF, DOCX, HTML, TXT, etc.)
    - **index_name**: Target index name for storing document chunks
    - **metadata**: Optional JSON string with additional metadata
    """
    # Determine MIME type
    mime_type = file.content_type
    if not mime_type or mime_type == "application/octet-stream":
        guessed, _ = mimetypes.guess_type(file.filename or "")
        mime_type = guessed or "text/plain"

    # Parse metadata
    extra_metadata: dict[str, Any] = {}
    if metadata:
        try:
            extra_metadata = json.loads(metadata)
        except json.JSONDecodeError:
            pass

    # Read file data
    data = await file.read()

    # Run the pipeline
    pipeline = DocumentPipeline(
        embedding_provider=provider,
        vector_store=store,
        config=settings.document,
    )
    result = await pipeline.process(
        data=data,
        filename=file.filename or "unknown",
        mime_type=mime_type,
        index_name=index_name,
        metadata=extra_metadata,
    )

    return DocumentUploadResponse(**result)


@router.delete("/documents", response_model=DeleteDocumentResponse)
async def delete_document(
    body: DeleteDocumentRequest,
    store: VectorStore = Depends(get_vector_store),
) -> DeleteDocumentResponse:
    """Delete a document and all its chunks from the vector store."""
    deleted = await store.delete_by_metadata(
        index_name=body.index_name,
        filters={"document_id": body.document_id},
    )

    return DeleteDocumentResponse(
        document_id=body.document_id,
        deleted_chunks=deleted,
        index_name=body.index_name,
    )
