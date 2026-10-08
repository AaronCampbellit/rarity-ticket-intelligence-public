export const DESIGN_SYSTEM_NAME = "Rarity";

export {
  MainContentIDProvider,
  useMainContentID,
} from "./foundations/mainContent";
export {
  PresentationProvider,
  useOptionalPresentation,
  usePresentation,
} from "./foundations/presentation";
export { Button, type ButtonProps } from "./components/actions/Button";
export { ButtonGroup } from "./components/actions/ButtonGroup";
export { IconButton } from "./components/actions/IconButton";
export { Field, type FieldProps } from "./components/fields/Field";
export { FormActions } from "./components/fields/FormActions";
export { Select } from "./components/fields/Select";
export { Switch } from "./components/fields/Switch";
export { Textarea } from "./components/fields/Textarea";
export { TextInput } from "./components/fields/TextInput";
export { Combobox, type ComboboxOption } from "./components/fields/Combobox";
export { MultiSelect } from "./components/fields/MultiSelect";
export { TagPicker, type TagPickerProps } from "./components/fields/TagPicker";
export { TrendChart } from "./components/data/TrendChart";
export { TagInput } from "./components/fields/TagInput";
export { DatePicker } from "./components/fields/DatePicker";
export { DateRangePicker } from "./components/fields/DateRangePicker";
export { TimeInput } from "./components/fields/TimeInput";
export { SearchInput } from "./components/fields/SearchInput";
export { ValidationSummary } from "./components/fields/ValidationSummary";
export {
  WriteOnlySecretField,
  type WriteOnlySecretFieldProps,
} from "./components/fields/WriteOnlySecretField";
export { Notice } from "./components/feedback/Notice";
export {
  StatePanel,
  type SharedState,
  type StatePanelProps,
} from "./components/feedback/StatePanel";
export {
  StatusBadge,
  type FeedbackTone,
} from "./components/feedback/StatusBadge";
export {
  ToastRegion,
  type ToastMessage,
} from "./components/feedback/ToastRegion";
export {
  AppShell,
  type NavigationItem,
} from "./components/navigation/AppShell";
export { GroupedSidebar } from "./components/navigation/GroupedSidebar";
export {
  RarityBrand,
  type RarityBrandProps,
} from "./components/navigation/RarityBrand";
export { TopBar } from "./components/navigation/TopBar";
export { CommandBar } from "./components/navigation/CommandBar";
export { PresentationMenu } from "./components/navigation/PresentationMenu";
export {
  readViewPreference,
  writeViewPreference,
  type SupportedWorkView,
  type ViewPreferenceRoute,
} from "./foundations/viewPreferences";
export { Menu, type MenuItem } from "./components/overlays/Menu";
export { Popover } from "./components/overlays/Popover";
export { Tooltip } from "./components/overlays/Tooltip";
import "./components/overlays/overlays.css";
export * from "./workspace";
export { Dialog, type DialogProps } from "./components/containers/Dialog";
export { Disclosure } from "./components/containers/Disclosure";
export { Drawer } from "./components/containers/Drawer";
export { Panel } from "./components/containers/Panel";
export { Page, type PageProps } from "./templates/Page";
export {
  DirectionPage,
  type DirectionPageProps,
} from "./templates/DirectionPage";
export { DataTable, type DataColumn } from "./components/data/DataTable";
export { FilterBar } from "./components/data/FilterBar";
export { Pagination } from "./components/data/Pagination";
export { Worklist, type WorklistProps } from "./components/data/Worklist";
export { ViewSwitcher, type WorkView } from "./components/data/ViewSwitcher";
export { KanbanBoard, type KanbanColumn } from "./components/data/KanbanBoard";
export { Timeline, type TimelineItem } from "./components/data/Timeline";
export { KeyValueList } from "./components/data/KeyValueList";
export { TagChip, type TagChipProps } from "./components/data/TagChip";
export { RecordWorkspace } from "./templates/RecordWorkspace";
export {
  ConditionBuilder,
  conditionsFromForm,
} from "./components/builders/ConditionBuilder";
export {
  WorkflowBuilder,
  workflowFromForm,
} from "./components/builders/WorkflowBuilder";
export {
  AutomationStepBuilder,
  automationStepsFromForm,
} from "./components/builders/AutomationStepBuilder";
export {
  DestinationBuilder,
  destinationsFromForm,
  type Destination,
} from "./components/builders/DestinationBuilder";
export {
  CalendarBuilder,
  calendarFromForm,
} from "./components/builders/CalendarBuilder";
export {
  KeyValueBuilder,
  recordFromForm,
} from "./components/builders/KeyValueBuilder";
export { ScopeBuilder } from "./components/builders/ScopeBuilder";
export { TextListBuilder } from "./components/builders/TextListBuilder";
export { ConnectionWizard } from "./components/builders/ConnectionWizard";
import "./components/builders/builders.css";
export { ConflictRecovery } from "./patterns/ConflictRecovery";
export {
  ReasonRequiredDialog,
  type ReasonEvidence,
} from "./patterns/ReasonRequiredDialog";
export { ScopedAction, type ScopeLabel } from "./patterns/ScopedAction";
export { VersionedEditor } from "./patterns/VersionedEditor";
export { ApprovalDecision } from "./patterns/ApprovalDecision";
export {
  AuditEvidence,
  type AuditEvidenceEntry,
} from "./patterns/AuditEvidence";
export {
  ConnectionCard,
  type ConnectionHealth,
} from "./patterns/ConnectionCard";
export {
  DurableJobProgress,
  type DurableJobState,
} from "./patterns/DurableJobProgress";
