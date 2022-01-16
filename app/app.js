import $ from 'jquery';
import feather from 'feather-icons';

/* @ngInject */
export function onStartupAngular($rootScope, $state, LocalStorage, cfpLoadingBar, $transitions, HttpRequestHelper, EndpointProvider) {
  $rootScope.$state = $state;
  const defaultTitle = document.title;

  $transitions.onEnter({}, () => {
    const endpoint = EndpointProvider.currentEndpoint();
    if (endpoint) {
      document.title = `${defaultTitle} | ${endpoint.Name}`;
    }
  });

  // Workaround to prevent the loading bar from going backward
  // https://github.com/chieffancypants/angular-loading-bar/issues/273
  const originalSet = cfpLoadingBar.set;
  cfpLoadingBar.set = function overrideSet(n) {
    if (n > cfpLoadingBar.status()) {
      originalSet.apply(cfpLoadingBar, arguments);
    }
  };

  $transitions.onBefore({}, () => {
    HttpRequestHelper.resetAgentHeaders();
  });

  $transitions.onSuccess({}, () => {
    feather.replace();
  });

  $(document).ajaxSend((event, jqXhr, jqOpts) => {
    const type = jqOpts.type === 'POST' || jqOpts.type === 'PUT' || jqOpts.type === 'PATCH';
    const hasNoContentType = jqOpts.contentType !== 'application/json' && jqOpts.headers && !jqOpts.headers['Content-Type'];
    if (type && hasNoContentType) {
      jqXhr.setRequestHeader('Content-Type', 'application/json');
    }
    jqXhr.setRequestHeader('Authorization', 'Bearer ' + LocalStorage.getJWT());
  });
}
