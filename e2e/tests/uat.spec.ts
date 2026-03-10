import { test, expect, APIRequestContext } from "@playwright/test";

// Shared state across sequential tests
let alice: { id: string; api_key: string };
let bob: { id: string; api_key: string };
let carol: { id: string; api_key: string };

const ADMIN_KEY = process.env.ADMIN_KEY || "SECRET";

// Helper: make authenticated API request
function authHeaders(apiKey: string) {
  return {
    Authorization: `Bearer ${apiKey}`,
    "Content-Type": "application/json",
  };
}

// ── Scenario 1: Health + Dashboard ──────────────────────────

test.describe("Scenario 1: Health + Dashboard", () => {
  test("1a. health check returns ok @api", async ({ request }) => {
    const res = await request.get("/api/health");
    expect(res.status()).toBe(200);
    expect(await res.json()).toEqual({ status: "ok" });
  });

  test("1b. dashboard loads @ui", async ({ page }) => {
    await page.goto("/");
    await expect(page).toHaveTitle(/agenthub/i);
    // Dashboard should have some content
    const body = await page.textContent("body");
    expect(body).toBeTruthy();
  });
});

// ── Scenario 2: Agent Registration ──────────────────────────

test.describe("Scenario 2: Agent Registration", () => {
  test("2a. register alice @api", async ({ request }) => {
    const res = await request.post("/api/register", {
      data: { id: "alice" },
    });
    expect(res.status()).toBe(201);
    alice = await res.json();
    expect(alice.id).toBe("alice");
    expect(alice.api_key).toBeTruthy();
    expect(alice.api_key.length).toBe(64); // hex-encoded 32 bytes
  });

  test("2b. register bob @api", async ({ request }) => {
    const res = await request.post("/api/register", {
      data: { id: "bob" },
    });
    expect(res.status()).toBe(201);
    bob = await res.json();
    expect(bob.id).toBe("bob");
  });

  test("2c. duplicate registration fails @api", async ({ request }) => {
    const res = await request.post("/api/register", {
      data: { id: "alice" },
    });
    expect(res.status()).toBe(409);
  });

  test("2d. invalid ID rejected @api", async ({ request }) => {
    const res = await request.post("/api/register", {
      data: { id: "bad agent!" },
    });
    expect(res.status()).toBe(400);
  });

  test("2e. reserved _system ID blocked @api", async ({ request }) => {
    const res = await request.post("/api/register", {
      data: { id: "_system" },
    });
    expect(res.status()).toBe(400);
  });
});

// ── Scenario 3: Admin Agent Creation ────────────────────────

test.describe("Scenario 3: Admin Agent Creation", () => {
  test("3a. admin creates agent @api", async ({ request }) => {
    const res = await request.post("/api/admin/agents", {
      headers: { Authorization: `Bearer ${ADMIN_KEY}` },
      data: { id: "charlie" },
    });
    expect(res.status()).toBe(201);
  });

  test("3b. wrong admin key rejected @api", async ({ request }) => {
    const res = await request.post("/api/admin/agents", {
      headers: { Authorization: "Bearer wrong-key" },
      data: { id: "mallory" },
    });
    expect(res.status()).toBe(401);
  });

  test("3c. reserved ID blocked via admin @api", async ({ request }) => {
    const res = await request.post("/api/admin/agents", {
      headers: { Authorization: `Bearer ${ADMIN_KEY}` },
      data: { id: "_sneaky" },
    });
    expect(res.status()).toBe(400);
    const body = await res.json();
    expect(body.error).toContain("reserved");
  });
});

// ── Scenario 4: Auth Enforcement ────────────────────────────

test.describe("Scenario 4: Auth Enforcement", () => {
  test("4a. no token returns 401 @api", async ({ request }) => {
    const res = await request.get("/api/channels");
    expect(res.status()).toBe(401);
  });

  test("4b. bogus token returns 401 @api", async ({ request }) => {
    const res = await request.get("/api/channels", {
      headers: { Authorization: "Bearer fake-token" },
    });
    expect(res.status()).toBe(401);
  });

  test("4c. valid token works @api", async ({ request }) => {
    const res = await request.get("/api/channels", {
      headers: authHeaders(alice.api_key),
    });
    expect(res.status()).toBe(200);
  });
});

// ── Scenario 5: Channels + Posts ────────────────────────────

test.describe("Scenario 5: Channels + Posts", () => {
  test("5a. create channel @api", async ({ request }) => {
    const res = await request.post("/api/channels", {
      headers: authHeaders(alice.api_key),
      data: { name: "results", description: "experiment results" },
    });
    expect(res.status()).toBe(201);
    const ch = await res.json();
    expect(ch.name).toBe("results");
  });

  test("5b. duplicate channel fails @api", async ({ request }) => {
    const res = await request.post("/api/channels", {
      headers: authHeaders(alice.api_key),
      data: { name: "results" },
    });
    expect(res.status()).toBe(409);
  });

  test("5c. list channels @api", async ({ request }) => {
    const res = await request.get("/api/channels", {
      headers: authHeaders(alice.api_key),
    });
    expect(res.status()).toBe(200);
    const channels = await res.json();
    expect(channels.length).toBeGreaterThanOrEqual(1);
    expect(channels.some((c: any) => c.name === "results")).toBe(true);
  });

  test("5d. post to channel @api", async ({ request }) => {
    const res = await request.post("/api/channels/results/posts", {
      headers: authHeaders(alice.api_key),
      data: { content: "AUROC 0.991 on af-benchmark" },
    });
    expect(res.status()).toBe(201);
    const post = await res.json();
    expect(post.agent_id).toBe("alice");
    expect(post.content).toBe("AUROC 0.991 on af-benchmark");
  });

  test("5e. bob replies @api", async ({ request }) => {
    const res = await request.post("/api/channels/results/posts", {
      headers: authHeaders(bob.api_key),
      data: { content: "nice! what training data?", parent_id: 1 },
    });
    expect(res.status()).toBe(201);
    const post = await res.json();
    expect(post.parent_id).toBe(1);
  });

  test("5f. get single post @api", async ({ request }) => {
    const res = await request.get("/api/posts/1", {
      headers: authHeaders(alice.api_key),
    });
    expect(res.status()).toBe(200);
    const post = await res.json();
    expect(post.content).toBe("AUROC 0.991 on af-benchmark");
  });

  test("5g. get replies @api", async ({ request }) => {
    const res = await request.get("/api/posts/1/replies", {
      headers: authHeaders(alice.api_key),
    });
    expect(res.status()).toBe(200);
    const replies = await res.json();
    expect(replies.length).toBe(1);
    expect(replies[0].content).toContain("training data");
  });

  test("5h. empty content rejected @api", async ({ request }) => {
    const res = await request.post("/api/channels/results/posts", {
      headers: authHeaders(alice.api_key),
      data: { content: "" },
    });
    expect(res.status()).toBe(400);
  });

  test("5i. post to nonexistent channel @api", async ({ request }) => {
    const res = await request.post("/api/channels/nonexistent/posts", {
      headers: authHeaders(alice.api_key),
      data: { content: "hello" },
    });
    expect(res.status()).toBe(404);
  });
});

// ── Scenario 6: Results + Leaderboard ───────────────────────

test.describe("Scenario 6: Results + Leaderboard", () => {
  test("6a. alice submits result @api", async ({ request }) => {
    const res = await request.post("/api/results", {
      headers: authHeaders(alice.api_key),
      data: {
        experiment: "af-detection",
        metric: "auroc",
        score: 0.991,
        platform: "a100",
      },
    });
    expect(res.status()).toBe(201);
    const result = await res.json();
    expect(result.agent_id).toBe("alice");
    expect(result.score).toBe(0.991);
  });

  test("6b. bob submits lower score @api", async ({ request }) => {
    const res = await request.post("/api/results", {
      headers: authHeaders(bob.api_key),
      data: {
        experiment: "af-detection",
        metric: "auroc",
        score: 0.884,
        platform: "a100",
      },
    });
    expect(res.status()).toBe(201);
  });

  test("6c. alice submits better score @api", async ({ request }) => {
    const res = await request.post("/api/results", {
      headers: authHeaders(alice.api_key),
      data: {
        experiment: "af-detection",
        metric: "auroc",
        score: 0.995,
        platform: "a100",
      },
    });
    expect(res.status()).toBe(201);
  });

  test("6d. leaderboard shows best per agent @api", async ({ request }) => {
    const res = await request.get(
      "/api/results/leaderboard?experiment=af-detection",
      { headers: authHeaders(alice.api_key) }
    );
    expect(res.status()).toBe(200);
    const board = await res.json();
    expect(board.length).toBe(2); // alice and bob
    expect(board[0].agent_id).toBe("alice");
    expect(board[0].score).toBe(0.995); // best score, not 0.991
    expect(board[1].agent_id).toBe("bob");
    expect(board[1].score).toBe(0.884);
  });

  test("6e. platform filter works @api", async ({ request }) => {
    // Add a result on different platform
    await request.post("/api/results", {
      headers: authHeaders(alice.api_key),
      data: {
        experiment: "af-detection",
        metric: "auroc",
        score: 0.5,
        platform: "m1",
      },
    });
    const res = await request.get(
      "/api/results/leaderboard?experiment=af-detection&platform=a100",
      { headers: authHeaders(alice.api_key) }
    );
    expect(res.status()).toBe(200);
    const board = await res.json();
    // m1 result should be excluded
    for (const r of board) {
      expect(r.platform).toBe("a100");
    }
  });

  test("6f. list all results (not deduplicated) @api", async ({ request }) => {
    const res = await request.get(
      "/api/results?experiment=af-detection",
      { headers: authHeaders(alice.api_key) }
    );
    expect(res.status()).toBe(200);
    const results = await res.json();
    expect(results.length).toBeGreaterThanOrEqual(3);
  });

  test("6g. experiment required @api", async ({ request }) => {
    const res = await request.post("/api/results", {
      headers: authHeaders(alice.api_key),
      data: { score: 0.5 },
    });
    expect(res.status()).toBe(400);
  });
});

// ── Scenario 7: Structured Memory ───────────────────────────

test.describe("Scenario 7: Structured Memory", () => {
  test("7a. alice records failure @api", async ({ request }) => {
    const res = await request.post("/api/memory", {
      headers: authHeaders(alice.api_key),
      data: {
        kind: "failure",
        content: "SAE sweep with homogeneous training gives 0.355",
        tags: "sae,training",
      },
    });
    expect(res.status()).toBe(201);
    const mem = await res.json();
    expect(mem.kind).toBe("failure");
  });

  test("7b. bob records fact @api", async ({ request }) => {
    const res = await request.post("/api/memory", {
      headers: authHeaders(bob.api_key),
      data: {
        kind: "fact",
        content: "diverse training data closes OOD gap",
        tags: "training,ood",
      },
    });
    expect(res.status()).toBe(201);
  });

  test("7c. alice records hunch @api", async ({ request }) => {
    const res = await request.post("/api/memory", {
      headers: authHeaders(alice.api_key),
      data: {
        kind: "hunch",
        content: "rank-1 LoRA might suffice",
        tags: "lora,af",
      },
    });
    expect(res.status()).toBe(201);
  });

  test("7d. invalid kind rejected @api", async ({ request }) => {
    const res = await request.post("/api/memory", {
      headers: authHeaders(alice.api_key),
      data: { kind: "guess", content: "this should fail" },
    });
    expect(res.status()).toBe(400);
    const body = await res.json();
    expect(body.error).toContain("kind must be");
  });

  test("7e. query by kind @api", async ({ request }) => {
    const res = await request.get("/api/memory?kind=failure", {
      headers: authHeaders(alice.api_key),
    });
    expect(res.status()).toBe(200);
    const mems = await res.json();
    expect(mems.length).toBeGreaterThanOrEqual(1);
    for (const m of mems) {
      expect(m.kind).toBe("failure");
    }
  });

  test("7f. query by tag @api", async ({ request }) => {
    const res = await request.get("/api/memory?tags=training", {
      headers: authHeaders(alice.api_key),
    });
    expect(res.status()).toBe(200);
    const mems = await res.json();
    expect(mems.length).toBeGreaterThanOrEqual(2); // alice's failure + bob's fact
  });

  test("7g. query by agent @api", async ({ request }) => {
    const res = await request.get("/api/memory?agent=bob", {
      headers: authHeaders(alice.api_key),
    });
    expect(res.status()).toBe(200);
    const mems = await res.json();
    for (const m of mems) {
      expect(m.agent_id).toBe("bob");
    }
  });
});

// ── Scenario 8: Typed Events + Playbooks ────────────────────

test.describe("Scenario 8: Typed Events + Playbooks", () => {
  test("8a. create alerts channel @api", async ({ request }) => {
    const res = await request.post("/api/channels", {
      headers: authHeaders(alice.api_key),
      data: { name: "alerts", description: "automated alerts" },
    });
    expect(res.status()).toBe(201);
  });

  test("8b. post RESULT event @api", async ({ request }) => {
    const res = await request.post("/api/events", {
      headers: authHeaders(alice.api_key),
      data: {
        event_type: "RESULT",
        payload: JSON.stringify({ experiment: "af-detection", score: 0.991 }),
        tags: "af",
      },
    });
    expect(res.status()).toBe(201);
    const event = await res.json();
    expect(event.event_type).toBe("RESULT");
  });

  test("8c. post FAILURE event @api", async ({ request }) => {
    const res = await request.post("/api/events", {
      headers: authHeaders(alice.api_key),
      data: {
        event_type: "FAILURE",
        payload: JSON.stringify({ experiment: "sae-sweep", error: "OOM" }),
        tags: "sae",
      },
    });
    expect(res.status()).toBe(201);
  });

  test("8d. invalid event type rejected @api", async ({ request }) => {
    const res = await request.post("/api/events", {
      headers: authHeaders(alice.api_key),
      data: { event_type: "INVALID", payload: "{}" },
    });
    expect(res.status()).toBe(400);
  });

  test("8e. list events by type @api", async ({ request }) => {
    const res = await request.get("/api/events?type=FAILURE", {
      headers: authHeaders(alice.api_key),
    });
    expect(res.status()).toBe(200);
    const events = await res.json();
    for (const e of events) {
      expect(e.event_type).toBe("FAILURE");
    }
  });

  test("8f. dead-end detector fires on 3 failures @api", async ({
    request,
  }) => {
    // Register carol
    const regRes = await request.post("/api/register", {
      data: { id: "carol" },
    });
    carol = await regRes.json();

    // 3 agents fail the same experiment
    for (const key of [alice.api_key, bob.api_key, carol.api_key]) {
      await request.post("/api/events", {
        headers: authHeaders(key),
        data: {
          event_type: "FAILURE",
          payload: JSON.stringify({ experiment: "dead-end-test" }),
        },
      });
    }

    // Wait for async playbook goroutine
    await new Promise((r) => setTimeout(r, 1000));

    // Check #alerts channel for dead-end-detector post
    const res = await request.get("/api/channels/alerts/posts", {
      headers: authHeaders(alice.api_key),
    });
    expect(res.status()).toBe(200);
    const posts = await res.json();
    const alert = posts.find(
      (p: any) =>
        p.content.includes("[dead-end-detector]") &&
        p.content.includes("dead-end-test")
    );
    expect(alert).toBeTruthy();
    expect(alert.agent_id).toBe("_system");
  });
});

// ── Scenario 9: Dashboard Content ───────────────────────────

test.describe("Scenario 9: Dashboard @ui", () => {
  test("9a. dashboard shows agent count", async ({ page }) => {
    await page.goto("/");
    const text = await page.textContent("body");
    // Should show agents we created (alice, bob, carol, charlie, _system)
    expect(text).toContain("Agents");
  });

  test("9b. dashboard shows recent posts", async ({ page }) => {
    await page.goto("/");
    const text = await page.textContent("body");
    expect(text).toContain("AUROC");
  });
});
