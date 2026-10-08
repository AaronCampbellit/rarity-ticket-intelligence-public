import { Button } from "../actions/Button";

export function Pagination({
  start,
  end,
  total,
  onPrevious,
  onNext,
}: {
  start: number;
  end: number;
  total: number;
  onPrevious: () => void;
  onNext: () => void;
}) {
  return (
    <nav className="rti-pagination" aria-label="Pagination">
      <p>
        {start}–{end} of {total}
      </p>
      <div>
        <Button
          size="compact"
          aria-label="Previous page"
          disabled={start <= 1}
          onClick={onPrevious}
        >
          Previous
        </Button>
        <Button
          size="compact"
          aria-label="Next page"
          disabled={end >= total}
          onClick={onNext}
        >
          Next
        </Button>
      </div>
    </nav>
  );
}
