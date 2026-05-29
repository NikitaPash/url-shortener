from typing import Optional

from pydantic import BaseModel, Field


class QueryRequest(BaseModel):
    question: str = Field(
        ...,
        min_length=3,
        max_length=500,
        examples=["How many clicks did I get today?"],
    )


class QueryResponse(BaseModel):
    question: str
    sql: str
    explanation: str
    columns: list[str]
    data: list[list]
    row_count: int
    error: Optional[str] = None
