// Config Generator for ntfy
(function () {
    "use strict";

    var CONFIG = [
        { key: "base-url", env: "NTFY_BASE_URL", section: "basic" },
        { key: "listen-http", env: "NTFY_LISTEN_HTTP", section: "basic", def: ":80" },
        { key: "behind-proxy", env: "NTFY_BEHIND_PROXY", section: "basic", type: "bool" },
        { key: "database-url", env: "NTFY_DATABASE_URL", section: "database" },
        { key: "auth-file", env: "NTFY_AUTH_FILE", section: "auth", def: "/var/lib/ntfy/auth.db" },
        { key: "auth-default-access", env: "NTFY_AUTH_DEFAULT_ACCESS", section: "auth" },
        { key: "enable-login", env: "NTFY_ENABLE_LOGIN", section: "auth", type: "bool" },
        { key: "enable-signup", env: "NTFY_ENABLE_SIGNUP", section: "auth", type: "bool" },
        { key: "attachment-cache-dir", env: "NTFY_ATTACHMENT_CACHE_DIR", section: "attach", def: "/var/cache/ntfy/attachments" },
        { key: "attachment-file-size-limit", env: "NTFY_ATTACHMENT_FILE_SIZE_LIMIT", section: "attach", def: "15M" },
        { key: "attachment-total-size-limit", env: "NTFY_ATTACHMENT_TOTAL_SIZE_LIMIT", section: "attach", def: "5G" },
        { key: "attachment-expiry-duration", env: "NTFY_ATTACHMENT_EXPIRY_DURATION", section: "attach", def: "3h" },
        { key: "cache-file", env: "NTFY_CACHE_FILE", section: "cache", def: "/var/cache/ntfy/cache.db" },
        { key: "cache-duration", env: "NTFY_CACHE_DURATION", section: "cache", def: "12h" },
        { key: "web-push-public-key", env: "NTFY_WEB_PUSH_PUBLIC_KEY", section: "webpush" },
        { key: "web-push-private-key", env: "NTFY_WEB_PUSH_PRIVATE_KEY", section: "webpush" },
        { key: "web-push-file", env: "NTFY_WEB_PUSH_FILE", section: "webpush", def: "/var/lib/ntfy/webpush.db" },
        { key: "web-push-email-address", env: "NTFY_WEB_PUSH_EMAIL_ADDRESS", section: "webpush" },
        { key: "smtp-sender-addr", env: "NTFY_SMTP_SENDER_ADDR", section: "smtp-out" },
        { key: "smtp-sender-from", env: "NTFY_SMTP_SENDER_FROM", section: "smtp-out" },
        { key: "smtp-sender-user", env: "NTFY_SMTP_SENDER_USER", section: "smtp-out" },
        { key: "smtp-sender-pass", env: "NTFY_SMTP_SENDER_PASS", section: "smtp-out" },
        { key: "smtp-server-listen", env: "NTFY_SMTP_SERVER_LISTEN", section: "smtp-in", def: ":25" },
        { key: "smtp-server-domain", env: "NTFY_SMTP_SERVER_DOMAIN", section: "smtp-in" },
        { key: "smtp-server-addr-prefix", env: "NTFY_SMTP_SERVER_ADDR_PREFIX", section: "smtp-in" },
        { key: "upstream-base-url", env: "NTFY_UPSTREAM_BASE_URL", section: "upstream" },
        { key: "enable-metrics", env: "NTFY_ENABLE_METRICS", section: "metrics", type: "bool" },
    ];

    var DOCKER_PATH_MAP = {
        "/var/cache/ntfy/cache.db": "/var/lib/ntfy/cache.db",
        "/var/cache/ntfy/attachments": "/var/lib/ntfy/attachments",
    };

    // Feature checkbox ID → detail section ID
    var FEATURE_MAP = {
        "cg-feat-cache": "cg-detail-cache",
        "cg-feat-attach": "cg-detail-attach",
        "cg-feat-webpush": "cg-detail-webpush",
        "cg-feat-smtp-out": "cg-detail-smtp-out",
        "cg-feat-smtp-in": "cg-detail-smtp-in",
    };

    function collectValues() {
        var values = {};
        var gen = document.getElementById("config-generator-app");
        if (!gen) return values;

        CONFIG.forEach(function (c) {
            var el = gen.querySelector('[data-key="' + c.key + '"]');
            if (!el) return;

            // Skip fields in hidden detail sections
            var section = el.closest(".cg-detail-section");
            if (section && section.style.display === "none") return;

            // Skip hidden individual fields (e.g. auth-file when using PostgreSQL)
            var field = el.closest(".cg-field");
            if (field && field.style.display === "none") return;

            var val;
            if (c.type === "bool") {
                if (el.checked) val = "true";
            } else {
                val = el.value.trim();
                if (!val) return;
            }
            if (val) values[c.key] = val;
        });

        // Provisioned users
        var userRows = gen.querySelectorAll(".cg-auth-user-row");
        var users = [];
        userRows.forEach(function (row) {
            var u = row.querySelector('[data-field="username"]');
            var p = row.querySelector('[data-field="password"]');
            var r = row.querySelector('[data-field="role"]');
            if (u && p && u.value.trim() && p.value.trim()) {
                users.push({ username: u.value.trim(), password: p.value.trim(), role: r ? r.value : "user" });
            }
        });
        if (users.length) values["_auth-users"] = users;

        // Provisioned ACLs
        var aclRows = gen.querySelectorAll(".cg-auth-acl-row");
        var acls = [];
        aclRows.forEach(function (row) {
            var u = row.querySelector('[data-field="username"]');
            var t = row.querySelector('[data-field="topic"]');
            var p = row.querySelector('[data-field="permission"]');
            if (u && t && t.value.trim()) {
                acls.push({ user: u.value.trim(), topic: t.value.trim(), permission: p ? p.value : "read-write" });
            }
        });
        if (acls.length) values["_auth-acls"] = acls;

        // Provisioned tokens
        var tokenRows = gen.querySelectorAll(".cg-auth-token-row");
        var tokens = [];
        tokenRows.forEach(function (row) {
            var u = row.querySelector('[data-field="username"]');
            var t = row.querySelector('[data-field="token"]');
            var l = row.querySelector('[data-field="label"]');
            if (u && t && u.value.trim() && t.value.trim()) {
                tokens.push({ user: u.value.trim(), token: t.value.trim(), label: l ? l.value.trim() : "" });
            }
        });
        if (tokens.length) values["_auth-tokens"] = tokens;

        return values;
    }

    function generateServerYml(values) {
        var lines = [];
        var sections = {
            basic: "# Server",
            database: "# Database",
            auth: "# Access control",
            attach: "# Attachments",
            cache: "# Message cache",
            webpush: "# Web push",
            "smtp-out": "# Email notifications (outgoing)",
            "smtp-in": "# Email publishing (incoming)",
            upstream: "# Upstream",
            metrics: "# Monitoring",
        };
        var lastSection = "";

        CONFIG.forEach(function (c) {
            if (!(c.key in values)) return;
            if (c.section !== lastSection) {
                if (lines.length) lines.push("");
                if (sections[c.section]) lines.push(sections[c.section]);
                lastSection = c.section;
            }
            var val = values[c.key];
            if (c.type === "bool") {
                lines.push(c.key + ": true");
            } else {
                lines.push(c.key + ': "' + val + '"');
            }
        });

        // Auth users
        if (values["_auth-users"]) {
            if (lastSection !== "auth") { lines.push(""); lines.push("# Access control"); }
            lines.push("auth-users:");
            values["_auth-users"].forEach(function (u) {
                lines.push("  - username: " + u.username);
                lines.push("    password: " + u.password);
                lines.push("    role: " + u.role);
            });
        }

        // Auth ACLs
        if (values["_auth-acls"]) {
            lines.push("auth-access:");
            values["_auth-acls"].forEach(function (a) {
                lines.push("  - user: " + (a.user || "*"));
                lines.push("    topic: " + a.topic);
                lines.push("    permission: " + a.permission);
            });
        }

        // Auth tokens
        if (values["_auth-tokens"]) {
            lines.push("auth-tokens:");
            values["_auth-tokens"].forEach(function (t) {
                lines.push("  - user: " + t.user);
                lines.push("    token: " + t.token);
                if (t.label) lines.push("    label: " + t.label);
            });
        }

        return lines.join("\n");
    }

    function dockerPath(p) {
        return DOCKER_PATH_MAP[p] || p;
    }

    function generateDockerCompose(values) {
        var lines = [];
        lines.push("services:");
        lines.push("  ntfy:");
        lines.push('    image: binwiederhier/ntfy');
        lines.push("    command: serve");
        lines.push("    environment:");

        CONFIG.forEach(function (c) {
            if (!(c.key in values)) return;
            var val = values[c.key];
            if (c.type === "bool") {
                val = "true";
            } else {
                // Adjust paths for Docker
                val = dockerPath(val);
            }
            // Double $ in bcrypt hashes
            if (val.indexOf("$") !== -1) {
                val = val.replace(/\$/g, "$$$$");
                lines.push("      # Note: $ is doubled to $$ for docker-compose");
            }
            lines.push("      " + c.env + ": " + val);
        });

        // Auth users in Docker
        if (values["_auth-users"]) {
            lines.push("      # Note: $ is doubled to $$ for docker-compose");
            values["_auth-users"].forEach(function (u, i) {
                var pw = u.password.replace(/\$/g, "$$$$");
                lines.push("      NTFY_AUTH_USERS_" + i + "_USERNAME: " + u.username);
                lines.push("      NTFY_AUTH_USERS_" + i + "_PASSWORD: " + pw);
                lines.push("      NTFY_AUTH_USERS_" + i + "_ROLE: " + u.role);
            });
        }

        // Auth ACLs in Docker
        if (values["_auth-acls"]) {
            values["_auth-acls"].forEach(function (a, i) {
                lines.push("      NTFY_AUTH_ACCESS_" + i + "_USER: " + (a.user || "*"));
                lines.push("      NTFY_AUTH_ACCESS_" + i + "_TOPIC: " + a.topic);
                lines.push("      NTFY_AUTH_ACCESS_" + i + "_PERMISSION: " + a.permission);
            });
        }

        // Auth tokens in Docker
        if (values["_auth-tokens"]) {
            values["_auth-tokens"].forEach(function (t, i) {
                var tok = t.token.replace(/\$/g, "$$$$");
                lines.push("      NTFY_AUTH_TOKENS_" + i + "_USER: " + t.user);
                lines.push("      NTFY_AUTH_TOKENS_" + i + "_TOKEN: " + tok);
                if (t.label) lines.push("      NTFY_AUTH_TOKENS_" + i + "_LABEL: " + t.label);
            });
        }

        lines.push("    volumes:");
        lines.push("      - ./:/var/lib/ntfy");
        lines.push("    ports:");

        var listen = values["listen-http"] || ":80";
        var port = listen.replace(/.*:/, "");
        lines.push('      - "8080:' + port + '"');

        return lines.join("\n");
    }

    function updateOutput() {
        var gen = document.getElementById("config-generator-app");
        if (!gen) return;

        var values = collectValues();
        var codeEl = gen.querySelector("#cg-code");
        if (!codeEl) return;

        var activeTab = gen.querySelector(".cg-tab.active");
        var format = activeTab ? activeTab.getAttribute("data-format") : "server-yml";

        var hasValues = false;
        for (var k in values) {
            if (values.hasOwnProperty(k)) { hasValues = true; break; }
        }

        if (!hasValues) {
            codeEl.innerHTML = '<span class="cg-empty-msg">Configure options on the left to generate your config...</span>';
            return;
        }

        var output = "";
        if (format === "docker-compose") {
            output = generateDockerCompose(values);
        } else {
            output = generateServerYml(values);
        }

        codeEl.textContent = output;
    }

    // Set a field's value only if it is currently empty
    function prefill(gen, key, value) {
        var el = gen.querySelector('[data-key="' + key + '"]');
        if (el && !el.value.trim()) el.value = value;
    }

    // Set a select's value (always, to reflect wizard state)
    function prefillSelect(gen, key, value) {
        var el = gen.querySelector('[data-key="' + key + '"]');
        if (el) el.value = value;
    }

    function updateVisibility() {
        var gen = document.getElementById("config-generator-app");
        if (!gen) return;

        var isPostgres = gen.querySelector('input[name="cg-db-type"][value="postgres"]');
        isPostgres = isPostgres && isPostgres.checked;

        var isPrivate = gen.querySelector('input[name="cg-server-type"][value="private"]');
        isPrivate = isPrivate && isPrivate.checked;

        var cacheEnabled = gen.querySelector("#cg-feat-cache");
        cacheEnabled = cacheEnabled && cacheEnabled.checked;

        var attachEnabled = gen.querySelector("#cg-feat-attach");
        attachEnabled = attachEnabled && attachEnabled.checked;

        var webpushEnabled = gen.querySelector("#cg-feat-webpush");
        webpushEnabled = webpushEnabled && webpushEnabled.checked;

        var smtpOutEnabled = gen.querySelector("#cg-feat-smtp-out");
        smtpOutEnabled = smtpOutEnabled && smtpOutEnabled.checked;

        var smtpInEnabled = gen.querySelector("#cg-feat-smtp-in");
        smtpInEnabled = smtpInEnabled && smtpInEnabled.checked;

        // Show database question only if a DB-dependent feature is selected
        var needsDb = isPrivate || cacheEnabled || webpushEnabled;
        var dbStep = gen.querySelector("#cg-wizard-db");
        if (dbStep) dbStep.style.display = needsDb ? "" : "none";

        // Database detail section (PostgreSQL only; SQLite needs no extra config)
        var pgSection = gen.querySelector("#cg-detail-db-postgres");
        if (pgSection) pgSection.style.display = (needsDb && isPostgres) ? "" : "none";

        // Hide cache-file in message cache section when PostgreSQL
        var cacheFileField = gen.querySelector("#cg-cache-file-field");
        if (cacheFileField) cacheFileField.style.display = isPostgres ? "none" : "";

        // Auth detail section
        var authSection = gen.querySelector("#cg-detail-auth");
        if (authSection) authSection.style.display = isPrivate ? "" : "none";

        // Hide auth-file and web-push-file if PostgreSQL
        var authFile = gen.querySelector('[data-key="auth-file"]');
        if (authFile) {
            var authField = authFile.closest(".cg-field");
            if (authField) authField.style.display = isPostgres ? "none" : "";
        }
        var wpFile = gen.querySelector('[data-key="web-push-file"]');
        if (wpFile) {
            var wpField = wpFile.closest(".cg-field");
            if (wpField) wpField.style.display = isPostgres ? "none" : "";
        }

        // Feature toggles → detail sections
        for (var featId in FEATURE_MAP) {
            var checkbox = gen.querySelector("#" + featId);
            var section = gen.querySelector("#" + FEATURE_MAP[featId]);
            if (checkbox && section) {
                section.style.display = checkbox.checked ? "" : "none";
            }
        }

        // Upstream special handling
        var upstreamCheck = gen.querySelector("#cg-feat-upstream");
        var upstreamInput = gen.querySelector('[data-key="upstream-base-url"]');
        if (upstreamCheck && upstreamInput) {
            upstreamInput.value = upstreamCheck.checked ? "https://ntfy.sh" : "";
        }

        // Metrics special handling
        var metricsCheck = gen.querySelector("#cg-feat-metrics");
        var metricsInput = gen.querySelector('[data-key="enable-metrics"]');
        if (metricsCheck && metricsInput) {
            metricsInput.checked = metricsCheck.checked;
        }

        // --- Pre-fill defaults based on wizard selections ---

        // Database
        if (isPostgres) {
            prefill(gen, "database-url", "postgres://user:pass@host:5432/ntfy");
        }

        // Access control: always sync default-access with open/private
        if (isPrivate) {
            prefillSelect(gen, "auth-default-access", "deny-all");
            if (!isPostgres) prefill(gen, "auth-file", "/var/lib/ntfy/auth.db");
        } else {
            prefillSelect(gen, "auth-default-access", "read-write");
        }

        // Persistent message cache
        if (cacheEnabled) {
            if (!isPostgres) prefill(gen, "cache-file", "/var/cache/ntfy/cache.db");
            prefill(gen, "cache-duration", "12h");
        }

        // Attachments
        if (attachEnabled) {
            prefill(gen, "attachment-cache-dir", "/var/cache/ntfy/attachments");
            prefill(gen, "attachment-file-size-limit", "15M");
            prefill(gen, "attachment-total-size-limit", "5G");
            prefill(gen, "attachment-expiry-duration", "3h");
        }

        // Web push
        if (webpushEnabled) {
            if (!isPostgres) prefill(gen, "web-push-file", "/var/lib/ntfy/webpush.db");
            prefill(gen, "web-push-email-address", "admin@example.com");
        }

        // Email notifications (outgoing)
        if (smtpOutEnabled) {
            prefill(gen, "smtp-sender-addr", "smtp.example.com:587");
            prefill(gen, "smtp-sender-from", "ntfy@example.com");
        }

        // Email publishing (incoming)
        if (smtpInEnabled) {
            prefill(gen, "smtp-server-listen", ":25");
            prefill(gen, "smtp-server-domain", "ntfy.example.com");
        }
    }

    function addRepeatableRow(container, type) {
        var row = document.createElement("div");
        row.className = "cg-repeatable-row cg-auth-" + type + "-row";

        if (type === "user") {
            row.innerHTML =
                '<input type="text" data-field="username" placeholder="Username">' +
                '<input type="text" data-field="password" placeholder="Password hash (bcrypt)">' +
                '<select data-field="role"><option value="user">user</option><option value="admin">admin</option></select>' +
                '<button type="button" class="cg-btn-remove" title="Remove">&times;</button>';
        } else if (type === "acl") {
            row.innerHTML =
                '<input type="text" data-field="username" placeholder="Username (* for everyone)">' +
                '<input type="text" data-field="topic" placeholder="Topic pattern">' +
                '<select data-field="permission"><option value="read-write">read-write</option><option value="read-only">read-only</option><option value="write-only">write-only</option><option value="deny">deny</option></select>' +
                '<button type="button" class="cg-btn-remove" title="Remove">&times;</button>';
        } else if (type === "token") {
            row.innerHTML =
                '<input type="text" data-field="username" placeholder="Username">' +
                '<input type="text" data-field="token" placeholder="Token">' +
                '<input type="text" data-field="label" placeholder="Label (optional)">' +
                '<button type="button" class="cg-btn-remove" title="Remove">&times;</button>';
        }

        row.querySelector(".cg-btn-remove").addEventListener("click", function () {
            row.remove();
            updateOutput();
        });
        row.querySelectorAll("input, select").forEach(function (el) {
            el.addEventListener("input", updateOutput);
        });

        container.appendChild(row);
    }

    function initGenerator() {
        var gen = document.getElementById("config-generator-app");
        if (!gen) return;

        // Tab switching
        gen.querySelectorAll(".cg-tab").forEach(function (tab) {
            tab.addEventListener("click", function () {
                gen.querySelectorAll(".cg-tab").forEach(function (t) { t.classList.remove("active"); });
                tab.classList.add("active");
                updateOutput();
            });
        });

        // All form inputs trigger update
        gen.querySelectorAll("input, select").forEach(function (el) {
            var evt = (el.type === "checkbox" || el.type === "radio") ? "change" : "input";
            el.addEventListener(evt, function () {
                updateVisibility();
                updateOutput();
            });
        });

        // Add buttons for repeatable rows
        gen.querySelectorAll(".cg-btn-add").forEach(function (btn) {
            btn.addEventListener("click", function () {
                var type = btn.getAttribute("data-add-type");
                var container = btn.previousElementSibling;
                if (!container) container = btn.parentElement.querySelector(".cg-repeatable-container");
                addRepeatableRow(container, type);
            });
        });

        // Copy button
        var copyBtn = gen.querySelector("#cg-copy-btn");
        if (copyBtn) {
            var copyIcon = '<svg xmlns="http://www.w3.org/2000/svg" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="9" y="9" width="13" height="13" rx="2" ry="2"></rect><path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"></path></svg>';
            var checkIcon = '<svg xmlns="http://www.w3.org/2000/svg" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="20 6 9 17 4 12"></polyline></svg>';
            copyBtn.addEventListener("click", function () {
                var code = gen.querySelector("#cg-code");
                if (code && code.textContent) {
                    navigator.clipboard.writeText(code.textContent).then(function () {
                        copyBtn.innerHTML = checkIcon;
                        copyBtn.style.color = "var(--md-primary-fg-color)";
                        setTimeout(function () {
                            copyBtn.innerHTML = copyIcon;
                            copyBtn.style.color = "";
                        }, 2000);
                    });
                }
            });
        }

        // Pre-fill base-url if not on ntfy.sh
        var baseUrlInput = gen.querySelector('[data-key="base-url"]');
        if (baseUrlInput && !baseUrlInput.value.trim()) {
            var host = window.location.hostname;
            if (host && host.indexOf("ntfy.sh") === -1) {
                baseUrlInput.value = "https://ntfy.example.com";
            }
        }

        updateVisibility();
        updateOutput();
    }

    if (document.readyState === "loading") {
        document.addEventListener("DOMContentLoaded", initGenerator);
    } else {
        initGenerator();
    }
})();
