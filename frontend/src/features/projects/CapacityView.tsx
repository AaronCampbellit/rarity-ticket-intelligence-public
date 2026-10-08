import { formatMinutes } from "./view-model";
import type { CapacityResource } from "./types";
import "./projects.css";

export function CapacityView({ resources }: { resources: CapacityResource[] }) {
  return (
    <section className="project-panel" aria-labelledby="capacity-title">
      <div className="project-panel-heading">
        <div>
          <h2 id="capacity-title">Resource capacity</h2>
          <p>Availability, scheduled work, actual time, and overbooking</p>
        </div>
      </div>
      <div className="capacity-list">
        {resources.map((resource) => {
          const used = resource.scheduledMinutes + resource.actualMinutes;
          const percent =
            resource.availableMinutes > 0
              ? Math.min(
                  100,
                  Math.round((used / resource.availableMinutes) * 100),
                )
              : 0;
          return (
            <article key={resource.id}>
              <header>
                <div>
                  <strong>{resource.name}</strong>
                  <span>{resource.role}</span>
                </div>
                <span
                  className={
                    resource.overbookedMinutes > 0 ? "capacity-risk" : ""
                  }
                >
                  {resource.overbookedMinutes > 0
                    ? `${formatMinutes(resource.overbookedMinutes)} over`
                    : `${formatMinutes(resource.availableMinutes - used)} free`}
                </span>
              </header>
              <div
                className="capacity-track"
                role="meter"
                aria-label={`${resource.name} capacity`}
                aria-valuemin={0}
                aria-valuemax={Math.max(resource.availableMinutes, used)}
                aria-valuenow={used}
                aria-valuetext={`${formatMinutes(used)} used of ${formatMinutes(resource.availableMinutes)} available`}
              >
                <span style={{ width: `${percent}%` }} />
              </div>
              <footer>
                <span>
                  {formatMinutes(resource.scheduledMinutes)} scheduled
                </span>
                <span>{formatMinutes(resource.actualMinutes)} actual</span>
                <span>
                  {formatMinutes(resource.availableMinutes)} available
                </span>
              </footer>
            </article>
          );
        })}
      </div>
    </section>
  );
}
