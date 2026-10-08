import { Button } from "../components/actions/Button";
import { ButtonGroup } from "../components/actions/ButtonGroup";
import { StatePanel } from "../components/feedback/StatePanel";

export function ConflictRecovery({
  expectedVersion,
  currentVersion,
  onReload,
  onReview,
}: {
  expectedVersion: number;
  currentVersion: number;
  onReload: () => void;
  onReview: () => void;
}) {
  return (
    <StatePanel
      state="conflict"
      title="This record changed"
      description={`Your edit expected version ${expectedVersion}; the current version is ${currentVersion}. No changes were applied.`}
      action={
        <ButtonGroup label="Conflict recovery">
          <Button intent="primary" onClick={onReload}>
            Reload current version
          </Button>
          <Button onClick={onReview}>Review differences</Button>
        </ButtonGroup>
      }
    />
  );
}
