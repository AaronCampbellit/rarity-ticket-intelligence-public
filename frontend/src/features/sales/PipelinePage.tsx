import { Page, StatePanel } from "../../design-system";
import { formatMoney } from "./view-model";
import type { PipelineWorkspace } from "./types";
import "./sales.css";

export function PipelinePage({ pipeline }: { pipeline: PipelineWorkspace }) {
  const byStage = new Map(
    pipeline.stages.map((stage) => [
      stage.id,
      pipeline.opportunities.filter(
        (opportunity) => opportunity.stageID === stage.id,
      ),
    ]),
  );
  return (
    <Page
      className="sales-page"
      eyebrow="Sales pipeline"
      title={pipeline.name}
      description="Aging, ownership, probability, and expected value"
    >
      <div className="pipeline-board" aria-label={`${pipeline.name} stages`}>
        {pipeline.stages.length === 0 ? (
          <StatePanel
            state="empty"
            title="No pipeline stages"
            description="Configure pipeline stages before adding opportunities."
          />
        ) : null}
        {pipeline.stages.map((stage) => (
          <section key={stage.id} className="pipeline-stage">
            <header>
              <div>
                <h2>{stage.name}</h2>
                <span>{stage.probability}% probability</span>
              </div>
              <strong>{byStage.get(stage.id)?.length ?? 0}</strong>
            </header>
            <ol>
              {byStage.get(stage.id)?.map((opportunity) => (
                <li key={opportunity.id}>
                  <a href={`#/sales/opportunities/${opportunity.id}`}>
                    <strong>{opportunity.name}</strong>
                    <span>{opportunity.clientName}</span>
                    <dl>
                      <div>
                        <dt>Value</dt>
                        <dd>
                          {formatMoney(
                            opportunity.valueMinor,
                            opportunity.currency,
                          )}
                        </dd>
                      </div>
                      <div>
                        <dt>Age</dt>
                        <dd>{opportunity.ageDays}d</dd>
                      </div>
                    </dl>
                  </a>
                </li>
              ))}
            </ol>
          </section>
        ))}
      </div>
    </Page>
  );
}
