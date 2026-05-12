"""Pydantic schemas for the documents API."""

from __future__ import annotations

from typing import Any

from pydantic import BaseModel, Field


class DocumentUploadResponse(BaseModel):
    """Response after uploading and processing a document."""

    document_id: str
    chunk_count: int
    index_name: str


class DeleteDocumentRequest(BaseModel):
    """Request body for deleting a document and all its chunks."""

    document_id: str = Field(..., min_length=1, description="ID of the document to delete")
    index_name: str = Field(..., min_length=1, description="Index containing the document")


class DeleteDocumentResponse(BaseModel):
    """Response after deleting a document."""

    document_id: str
    deleted_chunks: int
    index_name: str


class BatchDocumentUploadResponse(BaseModel):
    """Response after uploading multiple documents."""

    results: list[DocumentUploadResponse]
    total_documents: int
    total_chunks: int
