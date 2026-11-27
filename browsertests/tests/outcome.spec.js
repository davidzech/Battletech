// The Complete mission dialog without a battle (outcome.js), on the test server's Drill campaign: an Aces-style mission whose dialog opens from the page, with
// an Active and a Killed in action box per pilot, the mission's objective, the MVP and Commit (confirm.js). Then the campaign History's Roll back
// (campaignhistory.js) undoes it.
const { test, expect, newAccount, startCampaign, recruit } = require('./helpers');

test('reports a mission with a benched and a killed pilot, then rolls it back', async ({ page }) => {
  await newAccount(page);
  await startCampaign(page, 'drill', ['Alrek', 'Katarina']);
  await recruit(page, 'drill', 'Jarek'); // 300 SP from the 5,000 SP warchest

  await page.goto('/drill/scenario_000/');
  await page.locator('#outcome-open').click();
  const out = page.locator('#outcome-dialog');
  await expect(out).toBeVisible();
  const card = (name) => out.locator('.outcome-pilot', { hasText: name });
  await expect(out.locator('.outcome-pilot')).toHaveCount(3);
  for (const name of ['Alrek', 'Katarina', 'Jarek']) await expect(card(name).getByLabel('Active')).toBeChecked();

  // Jarek sits this one out: unticking Active hides his other boxes.
  await card('Jarek').getByLabel('Active').uncheck();
  await expect(card('Jarek').getByLabel('Killed in action')).toBeHidden();
  // Katarina is killed in action.
  await card('Katarina').getByLabel('Killed in action').check();
  await out.getByLabel('Destroy the depot').check();

  // The preview: the objective fills the pool; Alrek takes 80 SP, Jarek half for being benched, Katarina nothing; the rest goes to the warchest.
  await expect(card('Alrek').locator('.outcome-preview')).toContainText('80');
  await expect(card('Jarek').locator('.outcome-preview')).toContainText('40');
  await expect(card('Katarina').locator('.outcome-preview')).toContainText('Killed in Action');
  await expect(out.locator('#outcome-total')).toContainText('Awarded400 SP');
  await expect(out.locator('#outcome-total')).toContainText('Assigned to pilots120 SP');
  await expect(out.locator('#outcome-total')).toContainText('Warchest4700 → 4980 SP');

  // Only a pilot who took part and lives can be the MVP, and one has to be chosen.
  await expect(out.locator('#outcome-mvp option:not([disabled])')).toHaveText(['Alrek']);
  await expect(out.locator('#outcome-commit')).toBeDisabled();
  await out.locator('#outcome-mvp').selectOption({ label: 'Alrek' });
  await expect(out.locator('#outcome-commit')).toBeEnabled();

  // Cancelling the confirmation keeps the dialog open; confirming commits.
  await out.locator('#outcome-commit').click();
  const confirm = page.getByRole('dialog', { name: 'Commit this outcome?' });
  await confirm.getByRole('button', { name: 'Cancel' }).click();
  await expect(confirm).toBeHidden();
  await expect(out).toBeVisible();
  await out.locator('#outcome-commit').click();
  await confirm.getByRole('button', { name: 'Commit' }).click();

  await expect(page.locator('#mission-log-state')).toHaveText('Completed');
  const summary = page.locator('#outcome-summary');
  await expect(summary).toContainText('MVP');
  await expect(summary).toContainText('benched');
  await expect(summary).toContainText('Killed in Action.');
  await expect(summary).toContainText('Keywords granted: Depot Gone');

  // History: roll back to the start (the oldest line with a checkpoint), through two confirmations.
  await page.goto('/drill/');
  await expect(page.locator('.roster-pilot')).toHaveCount(2); // Katarina has fallen
  await page.locator('#history-open').click();
  const history = page.locator('#history-dialog');
  await expect(history).toBeVisible();
  const rollbacks = history.locator('.history-rollback');
  await expect(rollbacks).toHaveCount(2); // the start and the completed mission
  await rollbacks.last().click();
  const first = page.getByRole('dialog', { name: 'Roll back the campaign?' });
  await first.getByRole('button', { name: 'Roll back' }).click();
  const second = page.getByRole('dialog', { name: 'Are you sure?' });
  await second.getByRole('button', { name: 'Yes, roll back' }).click();

  // The page reloads as it was just after the start: two pilots, Katarina among them, and the mission open again.
  await expect(page.locator('.roster-pilot')).toHaveText([/Alrek/, /Katarina/]);
  await expect(page.locator('#scenario-cards .card').first()).toContainText('Open');
});
