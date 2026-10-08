import type { ReactNode } from "react";
import { Maximize2 } from "lucide-react";

export type WorklistProps<T> = {
  items: T[];
  getID: (item: T) => string;
  selectedID?: string;
  getLabel: (item: T) => string;
  renderItem: (item: T, selected: boolean) => ReactNode;
  renderDetail: (item: T) => ReactNode;
  empty: ReactNode;
  onSelect: (item: T) => void;
  onOpenFull?: (item: T) => void;
};

export function Worklist<T>({
  items,
  getID,
  selectedID,
  getLabel,
  renderItem,
  renderDetail,
  empty,
  onSelect,
  onOpenFull,
}: WorklistProps<T>) {
  if (!items.length) return <div className="rti-worklist__empty">{empty}</div>;
  const selected = items.find((item) => getID(item) === selectedID);

  return (
    <div className="rti-worklist">
      <div className="rti-worklist__list" aria-label="Records">
        {items.map((item) => {
          const current = getID(item) === selectedID;
          return (
            <div className="rti-worklist__item-shell" key={getID(item)}>
              <button
                type="button"
                className="rti-worklist__item"
                aria-label={getLabel(item)}
                aria-current={current ? "true" : undefined}
                onClick={() => onSelect(item)}
                onDoubleClick={() => onOpenFull?.(item)}
              >
                {renderItem(item, current)}
              </button>
              {onOpenFull ? (
                <button
                  type="button"
                  className="rti-worklist__open"
                  aria-label={`Open full record ${getLabel(item)}`}
                  title="Open full record"
                  onClick={(event) => {
                    event.stopPropagation();
                    onOpenFull(item);
                  }}
                >
                  <Maximize2 size={15} aria-hidden="true" />
                </button>
              ) : null}
            </div>
          );
        })}
      </div>
      <div className="rti-worklist__detail">
        {selected ? renderDetail(selected) : null}
      </div>
    </div>
  );
}
