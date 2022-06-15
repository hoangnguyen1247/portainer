import { EnvironmentType } from '@/portainer/environments/types';
import {
  isAgentEnvironment,
  isEdgeEnvironment,
} from '@/portainer/environments/utils';

interface Props {
  type: EnvironmentType;
  version: string;
}

export function AgentVersionTag({ type, version }: Props) {
  if (!isAgentEnvironment(type)) {
    return null;
  }

  return (
    <span className="space-x-1">
      <span>
        + <i className="fa fa-bolt" aria-hidden="true" />
      </span>
      <span>{isEdgeEnvironment(type) ? 'Edge Agent' : 'Agent'}</span>

      <span>{version}</span>
    </span>
  );
}
