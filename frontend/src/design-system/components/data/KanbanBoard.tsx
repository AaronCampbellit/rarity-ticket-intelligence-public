import { ArrowLeft, ArrowRight, GripVertical, Maximize2 } from "lucide-react";
import type { ReactNode } from "react";

export type KanbanColumn<T> = {
  id: string;
  label: string;
  tone?: "neutral" | "info" | "warning" | "success";
  items: T[];
};

export function KanbanBoard<T>({
  columns,
  getID,
  getLabel,
  renderCard,
  onOpen,
  onOpenFull,
  onMove,
}: {
  columns: KanbanColumn<T>[];
  getID: (item: T) => string;
  getLabel: (item: T) => string;
  renderCard: (item: T) => ReactNode;
  onOpen: (item: T) => void;
  onOpenFull?: (item: T) => void;
  onMove?: (item: T, fromColumnID: string, toColumnID: string) => void;
}) {
  return (
    <div className="rti-kanban" aria-label="Kanban board">
      {columns.map((column, columnIndex) => (
        <section key={column.id} data-tone={column.tone}>
          <header>
            <span />
            <h2>{column.label}</h2>
            <b>{column.items.length}</b>
          </header>
          <div>
            {column.items.map((item) => (
              <article key={getID(item)}>
                <button
                  type="button"
                  className="rti-kanban__card"
                  aria-label={`Open ${getLabel(item)}`}
                  onClick={() => onOpen(item)}
                  onDoubleClick={() => onOpenFull?.(item)}
                >
                  <GripVertical size={14} aria-hidden="true" />
                  <div>{renderCard(item)}</div>
                </button>
                {onMove || onOpenFull ? (
                  <footer aria-label={`Move ${getLabel(item)}`}>
                    {onOpenFull ? (
                      <button
                        type="button"
                        aria-label={`Open full record ${getLabel(item)}`}
                        title="Open full record"
                        onClick={(event) => {
                          event.stopPropagation();
                          onOpenFull(item);
                        }}
                      >
                        <Maximize2 size={14} aria-hidden="true" />
                      </button>
                    ) : null}
                    {onMove ? (
                      <>
                        <button
                          type="button"
                          disabled={columnIndex === 0}
                          aria-label={`Move ${getLabel(item)} left`}
                          onClick={() =>
                            onMove(
                              item,
                              column.id,
                              columns[columnIndex - 1]?.id ?? column.id,
                            )
                          }
                        >
                          <ArrowLeft size={14} aria-hidden="true" />
                        </button>
                        <button
                          type="button"
                          disabled={columnIndex === columns.length - 1}
                          aria-label={`Move ${getLabel(item)} right`}
                          onClick={() =>
                            onMove(
                              item,
                              column.id,
                              columns[columnIndex + 1]?.id ?? column.id,
                            )
                          }
                        >
                          <ArrowRight size={14} aria-hidden="true" />
                        </button>
                      </>
                    ) : null}
                  </footer>
                ) : null}
              </article>
            ))}
            {!column.items.length ? (
              <p className="rti-kanban__empty">No work in this stage</p>
            ) : null}
          </div>
        </section>
      ))}
    </div>
  );
}
