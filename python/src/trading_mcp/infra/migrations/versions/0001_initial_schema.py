"""Initial schema: agents, positions, trades, portfolio_snapshots

Revision ID: 0001
Revises:
Create Date: 2026-03-21

"""
from typing import Sequence, Union

import sqlalchemy as sa
from alembic import op

revision: str = "0001"
down_revision: Union[str, None] = None
branch_labels: Union[str, Sequence[str], None] = None
depends_on: Union[str, Sequence[str], None] = None


def upgrade() -> None:
    op.create_table(
        "agents",
        sa.Column("id", sa.Integer(), autoincrement=True, nullable=False),
        sa.Column("agent_id", sa.Integer(), nullable=False),
        sa.Column("wallet_address", sa.Text(), nullable=False),
        sa.Column("registration_uri", sa.Text(), nullable=False),
        sa.Column("registered_at", sa.DateTime(timezone=True), nullable=False),
        sa.PrimaryKeyConstraint("id"),
    )

    op.create_table(
        "positions",
        sa.Column("id", sa.Integer(), autoincrement=True, nullable=False),
        sa.Column("asset", sa.Text(), nullable=False),
        sa.Column("status", sa.Text(), nullable=False),
        sa.Column("action", sa.Text(), nullable=False),
        sa.Column("entry_price", sa.Numeric(20, 8), nullable=False),
        sa.Column("size_usd", sa.Numeric(20, 8), nullable=False),
        sa.Column("size_pct", sa.Numeric(10, 8), nullable=False),
        sa.Column("stop_loss", sa.Numeric(20, 8), nullable=False),
        sa.Column("take_profit", sa.Numeric(20, 8), nullable=False),
        sa.Column("strategy", sa.Text(), nullable=False),
        sa.Column("trigger_reason", sa.Text(), nullable=False),
        sa.Column("reasoning", sa.Text(), nullable=False),
        sa.Column("validation_uri", sa.Text(), nullable=True),
        sa.Column("opened_at", sa.DateTime(timezone=True), nullable=False),
        sa.Column("closed_at", sa.DateTime(timezone=True), nullable=True),
        sa.Column("exit_reason", sa.Text(), nullable=True),
        sa.Column("exit_price", sa.Numeric(20, 8), nullable=True),
        sa.Column("realized_pnl_usd", sa.Numeric(20, 8), nullable=True),
        sa.Column("realized_pnl_pct", sa.Numeric(10, 8), nullable=True),
        sa.Column("tx_hash_open", sa.Text(), nullable=False),
        sa.Column("tx_hash_close", sa.Text(), nullable=True),
        sa.PrimaryKeyConstraint("id"),
    )

    op.create_table(
        "trades",
        sa.Column("id", sa.Integer(), autoincrement=True, nullable=False),
        sa.Column("position_id", sa.Integer(), nullable=False),
        sa.Column("direction", sa.Text(), nullable=False),
        sa.Column("asset", sa.Text(), nullable=False),
        sa.Column("price", sa.Numeric(20, 8), nullable=False),
        sa.Column("size_usd", sa.Numeric(20, 8), nullable=False),
        sa.Column("slippage_pct", sa.Numeric(10, 8), nullable=False),
        sa.Column("tx_hash", sa.Text(), nullable=False),
        sa.Column("executed_at", sa.DateTime(timezone=True), nullable=False),
        sa.ForeignKeyConstraint(["position_id"], ["positions.id"]),
        sa.PrimaryKeyConstraint("id"),
    )

    op.create_table(
        "portfolio_snapshots",
        sa.Column("id", sa.Integer(), autoincrement=True, nullable=False),
        sa.Column("total_usd", sa.Numeric(20, 8), nullable=False),
        sa.Column("stablecoin_balance", sa.Numeric(20, 8), nullable=False),
        sa.Column("open_position_count", sa.Integer(), nullable=False),
        sa.Column("realized_pnl_today", sa.Numeric(20, 8), nullable=False),
        sa.Column("peak_total_usd", sa.Numeric(20, 8), nullable=False),
        sa.Column("current_drawdown_pct", sa.Numeric(10, 8), nullable=False),
        sa.Column("snapshotted_at", sa.DateTime(timezone=True), nullable=False),
        sa.PrimaryKeyConstraint("id"),
    )


def downgrade() -> None:
    op.drop_table("portfolio_snapshots")
    op.drop_table("trades")
    op.drop_table("positions")
    op.drop_table("agents")
