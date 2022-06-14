import { useQuery } from 'react-query';

import {
  EnvironmentsQueryParams,
  getAgentVersions,
} from '../environment.service';

export function useAgentVersionsList(
  query: Omit<EnvironmentsQueryParams, 'agentVersions'> = {}
) {
  return useQuery(['environments', 'agentVersions', query], () =>
    getAgentVersions(query)
  );
}
