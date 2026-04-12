from datetime import datetime
from typing import Optional

from sqlalchemy import DateTime, Integer, Text
from sqlalchemy.orm import Mapped, mapped_column

from trading_mcp.domain.entity.base import Base


class Agent(Base):
    __tablename__ = "agents"

    id: Mapped[int] = mapped_column(Integer, primary_key=True, autoincrement=True)
    agent_id: Mapped[int] = mapped_column(Integer, nullable=False)
    wallet_address: Mapped[str] = mapped_column(Text, nullable=False)
    registration_uri: Mapped[str] = mapped_column(Text, nullable=False)
    registered_at: Mapped[datetime] = mapped_column(DateTime(timezone=True), nullable=False)

    def __repr__(self) -> str:
        return f"Agent(id={self.id}, agent_id={self.agent_id}, wallet={self.wallet_address!r})"

    # Optional for type checker — agent_id is 0 in paper mode before ERC-8004 registration
    @classmethod
    def create(
        cls,
        agent_id: int,
        wallet_address: str,
        registration_uri: str,
        registered_at: Optional[datetime] = None,
    ) -> "Agent":
        from datetime import timezone

        return cls(
            agent_id=agent_id,
            wallet_address=wallet_address,
            registration_uri=registration_uri,
            registered_at=registered_at or datetime.now(tz=timezone.utc),
        )
