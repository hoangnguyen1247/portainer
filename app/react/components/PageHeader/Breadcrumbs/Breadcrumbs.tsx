import { Fragment } from 'react';

import { Link } from '@@/Link';

import './Breadcrumbs.css';

export interface Crumb {
  label: string;
  link?: string;
  linkParams?: Record<string, unknown>;
}
interface Props {
  breadcrumbs: (Crumb | string | number)[];
}

export function Breadcrumbs({ breadcrumbs }: Props) {
  return (
    <div className="breadcrumb-links font-medium text-gray-7">
      {breadcrumbs.map((crumb, index) => (
        <Fragment key={index}>
          {renderCrumb(crumb)}
          {index !== breadcrumbs.length - 1 ? ' > ' : ''}
        </Fragment>
      ))}
    </div>
  );
}

function renderCrumb(crumb: Crumb | string | number) {
  if (typeof crumb !== 'object') {
    return crumb;
  }

  if (crumb.link) {
    return (
      <Link
        to={crumb.link}
        params={crumb.linkParams}
        className="text-blue-9 hover:underline"
      >
        {crumb.label}
      </Link>
    );
  }

  return crumb.label;
}
