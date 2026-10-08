# Dependency Graph

**Status:** Draft

The dependency graph is a core capability linking assets, services, locations, clients, and relevant operational objects.

Supported relationship families include depends on, runs on, connects through, authenticates through, hosted by, backed up by, monitored by, managed by, used by, and impacts.

Each edge records direction, source, confidence, creator, last verification, effective dates, visibility, notes, and version history. V1 supports manual, API-created, and integration-discovered relationships; upstream/downstream traversal; cycle validation; impact previews; and ticket linkage.

Confirmed impact and predicted impact are always distinct. Prediction never silently becomes fact. Later correlation and root-cause analysis build on trustworthy graph data.
