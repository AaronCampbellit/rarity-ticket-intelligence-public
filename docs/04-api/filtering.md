# Filtering

**Status:** Draft

Filtering uses documented typed fields and operators, nested relationship rules where safe, bounded complexity, and explicit client scope. Unsupported filters return structured errors rather than being ignored.

Search text and structured filters may combine. Authorization is applied before results are returned and cannot be weakened by filter expressions.
