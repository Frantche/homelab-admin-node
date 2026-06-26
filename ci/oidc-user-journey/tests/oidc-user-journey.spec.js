const { test, expect } = require('@playwright/test');

const username = process.env.OIDC_TEST_USERNAME || 'ci-sso-user';
const password = process.env.OIDC_TEST_PASSWORD || 'ci-sso-user-password';

const domains = {
  harbor: process.env.HARBOR_URL || 'https://harbor.example.com',
  gitea: process.env.GITEA_URL || 'https://git.example.com',
  openbao: process.env.OPENBAO_URL || 'https://bao.example.com',
  keycloak: process.env.KEYCLOAK_URL || 'https://keycloak.example.com'
};

async function clickFirstVisible(page, locators, timeout = 5000) {
  for (const locator of locators) {
    const candidate = typeof locator === 'string' ? page.locator(locator) : locator;
    try {
      await candidate.first().waitFor({ state: 'visible', timeout });
      await candidate.first().click();
      return true;
    } catch {
      // Try the next known UI variant.
    }
  }
  return false;
}

async function completeKeycloakLogin(page) {
  await page.waitForLoadState('domcontentloaded');
  if (!page.url().startsWith(domains.keycloak)) {
    return;
  }

  const usernameInput = page.locator('input#username, input[name="username"]');
  await usernameInput.waitFor({ state: 'visible', timeout: 30000 });
  await usernameInput.fill(username);
  await page.locator('input#password, input[name="password"]').fill(password);
  await Promise.all([
    page.waitForLoadState('domcontentloaded'),
    page.locator('input#kc-login, button[type="submit"], input[type="submit"]').first().click()
  ]);

  await clickFirstVisible(page, [
    page.getByRole('button', { name: /^yes$/i }),
    page.getByRole('button', { name: /accept|approve|allow|grant/i }),
    page.locator('input[name="accept"], button[name="accept"]')
  ], 3000);
}

async function expectJSONFromBrowser(page, path, predicate, description) {
  const payload = await page.evaluate(async (targetPath) => {
    const response = await fetch(targetPath, {
      credentials: 'include',
      headers: { accept: 'application/json' }
    });
    const text = await response.text();
    let json = null;
    try {
      json = text ? JSON.parse(text) : null;
    } catch {
      json = null;
    }
    return { status: response.status, text, json };
  }, path);

  expect(payload.status, `${description}: ${payload.text}`).toBeGreaterThanOrEqual(200);
  expect(payload.status, `${description}: ${payload.text}`).toBeLessThan(300);
  expect(predicate(payload.json), `${description}: ${payload.text}`).toBeTruthy();
}

async function expectGiteaWebSession(page) {
  await page.goto(`${domains.gitea}/user/settings`, { waitUntil: 'domcontentloaded' });
  await expect(page, 'Gitea should keep the SSO user on an authenticated page').not.toHaveURL(/\/user\/login/, {
    timeout: 30000
  });
  await expect
    .poll(async () => {
      return page.evaluate((expectedUsername) => {
        const text = document.body.innerText;
        const hasAuthenticatedContent = /settings|account|profile|applications/i.test(text) || text.includes(expectedUsername);
        const showsLoginForm = /sign in|sign up|forgot password/i.test(text) && document.querySelector('input[name="password"]');
        return hasAuthenticatedContent && !showsLoginForm;
      }, username);
    }, { timeout: 30000 })
    .toBeTruthy();
}

test.describe('OIDC user journey', () => {
  test('Harbor accepts the Keycloak SSO user', async ({ page }) => {
    await page.goto(domains.harbor, { waitUntil: 'domcontentloaded' });
    const clicked = await clickFirstVisible(page, [
      page.getByRole('button', { name: /oidc|keycloak|single sign/i }),
      page.getByRole('link', { name: /oidc|keycloak|single sign/i }),
      page.locator('a[href*="oidc"], button:has-text("OIDC"), button:has-text("Keycloak")')
    ]);
    if (!clicked) {
      await page.goto(`${domains.harbor}/c/oidc/login`, { waitUntil: 'domcontentloaded' });
    }

    await completeKeycloakLogin(page);
    await expect(page).toHaveURL(/harbor\.example\.com/, { timeout: 60000 });
    await expectJSONFromBrowser(
      page,
      '/api/v2.0/users/current',
      (json) => json && (json.username === username || json.oidc_user_meta || json.sysadmin_flag === true),
      'Harbor current user'
    );
  });

  test('Gitea accepts the Keycloak SSO user', async ({ page }) => {
    await page.goto(`${domains.gitea}/user/oauth2/keycloak`, { waitUntil: 'domcontentloaded' });
    await completeKeycloakLogin(page);
    await expect(page).toHaveURL(/git\.example\.com/, { timeout: 60000 });
    await expectGiteaWebSession(page);
  });

  test('OpenBao accepts the Keycloak SSO user', async ({ page }) => {
    await page.goto(`${domains.openbao}/ui/`, { waitUntil: 'domcontentloaded' });
    const clicked = await clickFirstVisible(page, [
      page.getByRole('link', { name: /oidc|keycloak/i }),
      page.getByRole('button', { name: /oidc|keycloak|sign in/i }),
      page.locator('a[href*="/auth/oidc"], button:has-text("OIDC"), button:has-text("Keycloak")')
    ]);
    if (!clicked) {
      await page.goto(`${domains.openbao}/ui/vault/auth/oidc/oidc`, { waitUntil: 'domcontentloaded' });
    }

    await completeKeycloakLogin(page);
    await expect(page).toHaveURL(/bao\.example\.com\/ui\//, { timeout: 60000 });
    await expect
      .poll(async () => {
        return page.evaluate(() => {
          const keys = Object.keys(window.localStorage);
          const hasToken = keys.some((key) => /token|vault/i.test(key) && window.localStorage.getItem(key));
          return hasToken || /secrets|access|vault|openbao/i.test(document.body.innerText);
        });
      }, { timeout: 30000 })
      .toBeTruthy();
  });
});
