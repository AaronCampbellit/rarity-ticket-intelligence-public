export type Money = {
  minor: number;
  currency: string;
};

export type PipelineStage = {
  id: string;
  name: string;
  probability: number;
  ageDays: number;
  requiresProposal?: boolean;
  requiresApproval?: boolean;
};

export type ProposalVersionSummary = {
  id: string;
  version: number;
  state: "draft" | "issued" | "accepted" | "declined" | "expired";
  totalMinor: number;
  marginMinor: number;
  currency: string;
  issuedAt: string | null;
  acceptance: {
    method: "electronic" | "offline";
    signerName: string;
    acceptedAt: string;
  } | null;
};

export type OpportunityWorkspace = {
  id: string;
  name: string;
  clientName: string;
  ownerName: string;
  teamName: string;
  stage: PipelineStage;
  nextStage?: PipelineStage;
  expectedValueMinor: number;
  currency: string;
  expectedCloseOn: string;
  version: number;
  activities: Array<{ id: string; at: string; text: string }>;
  tasks: Array<{ id: string; title: string; completed: boolean }>;
  proposalVersions: ProposalVersionSummary[];
};

export type ProposalLineType =
  "fixed_fee" | "time_and_materials" | "product_license" | "recurring_service";

export type ProposalLine = {
  id: string;
  type: ProposalLineType;
  description: string;
  quantity: number;
  unitPriceMinor: number;
  unitCostMinor: number;
  discountMinor: number;
  plannedMinutes: number;
};

export type ProposalWorkspace = {
  id: string;
  currency: string;
  approvalState: "not_required" | "required" | "approved" | "rejected";
  versions: ProposalVersionSummary[];
  lines: ProposalLine[];
};

export type PipelineOpportunity = {
  id: string;
  name: string;
  clientName: string;
  ownerName: string;
  valueMinor: number;
  currency: string;
  ageDays: number;
  stageID: string;
};

export type PipelineWorkspace = {
  id: string;
  name: string;
  stages: PipelineStage[];
  opportunities: PipelineOpportunity[];
};

export type ForecastRow = {
  ownerID: string;
  ownerName: string;
  pipelineMinor: number;
  weightedMinor: number;
  committedMinor: number;
  currency: string;
};
