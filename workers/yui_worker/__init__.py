"""Yui AI workers.

Workers are separate processes that do one ML job each behind a narrow
contract (SRS 4.1, ADR-005). A worker receives only its task payload: it has
no database access and it cannot invoke tools (VIS-009).
"""

__version__ = "0.1.0"
