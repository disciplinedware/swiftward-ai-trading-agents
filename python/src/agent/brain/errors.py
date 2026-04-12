from common.exceptions import AgentError


class BrainError(AgentError):
    """Brain pipeline failure — invalid LLM response or unrecoverable stage error."""
