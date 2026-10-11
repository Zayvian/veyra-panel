// Delegate events so controls also work after htmx replaces the access form.
(function () {
  function boxes(scope) {
    return Array.from(scope.querySelectorAll('input[name="inbound"]'));
  }
  function refresh(form) {
    form.querySelectorAll('[data-access-group]').forEach(function (group) {
      var inputs = boxes(group);
      group.querySelector('[data-access-count]').textContent =
        '已选 ' + inputs.filter(function (input) { return input.checked; }).length + ' / ' + inputs.length;
    });
    var summary = form.querySelector('[data-access-summary]');
    if (summary) summary.textContent = form.elements.access_mode.value === 'all'
      ? '全部可用：以后新建的入站也会自动开放。保存后生效。'
      : '仅允许勾选项：以后新建的入站不会自动开放。保存后生效。';
  }
  document.addEventListener('click', function (event) {
    var button = event.target.closest('[data-access-action]');
    if (!button) return;
    var form = button.closest('[data-access-form]');
    if (!form) return;
    var action = button.dataset.accessAction;
    var group = button.closest('[data-access-group]');
    form.elements.access_mode.value = 'selected';
    if (action === 'all' || action === 'clear' || action === 'only') {
      boxes(form).forEach(function (input) { input.checked = action === 'all'; });
    }
    if (group) {
      boxes(group).forEach(function (input) { input.checked = action !== 'group-clear'; });
    }
    refresh(form);
  });
  document.addEventListener('change', function (event) {
    var form = event.target.closest('[data-access-form]');
    if (!form) return;
    if (event.target.name === 'inbound') form.elements.access_mode.value = 'selected';
    if (event.target.name === 'access_mode' && event.target.value === 'all') {
      boxes(form).forEach(function (input) { input.checked = true; });
    }
    refresh(form);
  });
})();
