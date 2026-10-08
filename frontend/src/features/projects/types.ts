export type ConversionTask = {
  id: string;
  title: string;
  version: number;
  completed: boolean;
};

export type ConversionPhase = {
  position: number;
  name: string;
  proposalLineIDs: string[];
  plannedMinutes: number;
  budgetMinor: number;
};

export type ConversionPreviewModel = {
  hash: string;
  opportunityID: string;
  proposalVersionID: string;
  client: {
    action: "match" | "create";
    clientID?: string;
    name: string;
  };
  projectDisplayID: string;
  projectName: string;
  currency: string;
  originalBudgetMinor: number;
  plannedMinutes: number;
  phases: ConversionPhase[];
  tasks: ConversionTask[];
};

export type CapacityResource = {
  id: string;
  name: string;
  role: string;
  availableMinutes: number;
  scheduledMinutes: number;
  actualMinutes: number;
  overbookedMinutes: number;
};

export type FinancialSummaryModel = {
  currency: string;
  originalBudgetMinor: number;
  currentBudgetMinor: number;
  plannedLaborMinor: number;
  actualLaborMinor: number;
  actualLaborComplete: boolean;
  costActualsMinor: number;
  committedCostMinor: number;
  billableWorkMinor: number;
  profitMinor: number;
  profitAvailable: boolean;
  projectedProfitMinor: number;
  marginBasisPoints: number;
};

export type ChangeOrderVersionModel = {
  id: string;
  version: number;
  description: string;
  currency: string;
  revenueDeltaMinor: number;
  costDeltaMinor: number;
  laborDeltaMinutes: number;
};

export type ChangeOrderWorkspace = {
  id: string;
  displayID: string;
  state: "draft" | "issued" | "approved" | "rejected" | "applied" | "cancelled";
  version: number;
  currentVersion?: ChangeOrderVersionModel;
  decisions: Array<{
    id: string;
    decision: "approved" | "rejected";
    override: boolean;
    reason: string;
    decidedBy: string;
    decidedAt: string;
  }>;
};

export type ProjectPhaseModel = {
  id: string;
  position: number;
  name: string;
  state: string;
  ownerName: string;
  participatingTeams: string[];
  plannedMinutes: number;
  actualMinutes: number;
  deliverables: string[];
  completionCriteria: string[];
  financials?: FinancialSummaryModel;
  tasks: Array<{
    id: string;
    title: string;
    ownerName: string;
    completed: boolean;
    subtasks: number;
    plannedMinutes: number;
    actualMinutes: number;
  }>;
};

export type ProjectWorkspace = {
  id: string;
  displayID: string;
  name: string;
  clientName: string;
  lifecycleState: string;
  plannedStart: string;
  plannedEnd: string;
  originalProposalVersion: number;
  phases: ProjectPhaseModel[];
  projectTasks?: Array<{
    id: string;
    title: string;
    status: string;
    ownerName: string;
    subtasks: number;
    plannedMinutes: number;
    actualMinutes: number;
  }>;
  resourcePlans?: Array<{
    id: string;
    resourceType: string;
    resourceName: string;
    startsOn: string;
    endsOn: string;
    plannedMinutes: number;
  }>;
  costActuals?: Array<{
    id: string;
    phaseID: string;
    costType: string;
    description: string;
    amountMinor: number;
    currency: string;
    committed: boolean;
    incurredAt: string;
  }>;
  capacity: CapacityResource[];
  financials: FinancialSummaryModel;
  financialsVisible?: boolean;
  changeOrders: ChangeOrderWorkspace[];
};
