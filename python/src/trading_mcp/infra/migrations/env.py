import asyncio
import concurrent.futures
import sys
from logging.config import fileConfig
from pathlib import Path

# Ensure python/src is on sys.path when alembic CLI runs from any working directory.
_src_dir = str(Path(__file__).parent.parent.parent.parent)
if _src_dir not in sys.path:
    sys.path.insert(0, _src_dir)

from alembic import context  # noqa: E402
from sqlalchemy import pool  # noqa: E402
from sqlalchemy.ext.asyncio import create_async_engine  # noqa: E402

# Side-effect import: registers all ORM models on Base.metadata
import trading_mcp.domain.entity  # noqa: E402, F401
from trading_mcp.domain.entity.base import Base  # noqa: E402, F401

config = context.config

if config.config_file_name is not None:
    fileConfig(config.config_file_name)

target_metadata = Base.metadata


def _get_url() -> str:
    url = config.get_main_option("sqlalchemy.url")
    if url:
        return url
    # Fall back to config.yaml when running via alembic CLI
    try:
        from common.config import get_config

        return get_config().trading.database_url
    except Exception as exc:
        raise RuntimeError(
            f"No sqlalchemy.url in alembic.ini and config.yaml failed: {exc}"
        ) from exc


def run_migrations_offline() -> None:
    url = _get_url()
    context.configure(
        url=url,
        target_metadata=target_metadata,
        literal_binds=True,
        dialect_opts={"paramstyle": "named"},
    )
    with context.begin_transaction():
        context.run_migrations()


def do_run_migrations(connection) -> None:
    context.configure(connection=connection, target_metadata=target_metadata)
    with context.begin_transaction():
        context.run_migrations()


async def run_migrations_online() -> None:
    url = _get_url()
    connectable = create_async_engine(url, poolclass=pool.NullPool)
    async with connectable.connect() as connection:
        await connection.run_sync(do_run_migrations)
    await connectable.dispose()


def _run_async_migrations() -> None:
    """Run async migrations safely regardless of whether a loop is already running.

    When called from an async context (e.g. FastMCP lifespan in Task 11),
    asyncio.run() would raise RuntimeError. We instead submit to a thread where
    no loop is running, so asyncio.run() works normally there.
    """
    try:
        asyncio.get_running_loop()
        # Already inside a running loop — run in a dedicated thread.
        with concurrent.futures.ThreadPoolExecutor(max_workers=1) as executor:
            executor.submit(asyncio.run, run_migrations_online()).result()
    except RuntimeError:
        # No running loop — safe to call asyncio.run() directly.
        asyncio.run(run_migrations_online())


if context.is_offline_mode():
    run_migrations_offline()
else:
    _run_async_migrations()
