// Config Generator for ntfy
(function () {
    "use strict";

    var CONFIG = [
        { key: "base-url", env: "NTFY_BASE_URL", section: "basic" },
        { key: "listen-http", env: "NTFY_LISTEN_HTTP", section: "basic", def: ":80" },
        { key: "behind-proxy", env: "NTFY_BEHIND_PROXY", section: "basic", type: "bool" },
        { key: "cache-file", env: "NTFY_CACHE_FILE", section: "database", def: "/var/cache/ntfy/cache.db" },
        { key: "database-url", env: "NTFY_DATABASE_URL", section: "database" },
        { key: "auth-file", env: "NTFY_AUTH_FILE", section: "auth", def: "/var/lib/ntfy/auth.db" },
        { key: "auth-default-access", env: "NTFY_AUTH_DEFAULT_ACCESS", section: "auth" },
        { key: "enable-login", env: "NTFY_ENABLE_LOGIN", section: "auth", type: "bool" },
        { key: "enable-signup", env: "NTFY_ENABLE_SIGNUP", section: "auth", type: "bool" },
        { key: "attachment-cache-dir", env: "NTFY_ATTACHMENT_CACHE_DIR", section: "attach", def: "/var/cache/ntfy/attachments" },
        { key: "attachment-file-size-limit", env: "NTFY_ATTACHMENT_FILE_SIZE_LIMIT", section: "attach", def: "15M" },
        { key: "attachment-total-size-limit", env: "NTFY_ATTACHMENT_TOTAL_SIZE_LIMIT", section: "attach", def: "5G" },
        { key: "attachment-expiry-duration", env: "NTFY_ATTACHMENT_EXPIRY_DURATION", section: "attach", def: "3h" },
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

    function collectValues() {
        var values = {};
        var gen = document.getElementById("config-generator-app");
        if (!gen) return values;

        var isPostgres = gen.querySelector('input[name="cg-db-type"][value="postgres"]');
        isPostgres = isPostgres && isPostgres.checked;

        CONFIG.forEach(function (c) {
            var el = gen.querySelector('[data-key="' + c.key + '"]');
            if (!el) return;

            // Skip hidden fields
            var container = el.closest(".cg-conditional");
            if (container && !container.classList.contains("visible")) return;
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
            lines.push("auth-default-access:");
            lines.push("  everyone:");
            values["_auth-acls"].forEach(function (a) {
                // This uses the topic-level provisioning format
            });
            // Actually use provisioned format
            lines.pop(); lines.pop();
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

    function generateEnvVars(values) {
        var lines = [];

        CONFIG.forEach(function (c) {
            if (!(c.key in values)) return;
            var val = values[c.key];
            if (c.type === "bool") val = "true";
            // Use single quotes if value contains $
            var q = val.indexOf("$") !== -1 ? "'" : '"';
            lines.push("export " + c.env + "=" + q + val + q);
        });

        if (values["_auth-users"]) {
            values["_auth-users"].forEach(function (u, i) {
                var q = u.password.indexOf("$") !== -1 ? "'" : '"';
                lines.push("export NTFY_AUTH_USERS_" + i + '_USERNAME="' + u.username + '"');
                lines.push("export NTFY_AUTH_USERS_" + i + "_PASSWORD=" + q + u.password + q);
                lines.push("export NTFY_AUTH_USERS_" + i + '_ROLE="' + u.role + '"');
            });
        }

        if (values["_auth-acls"]) {
            values["_auth-acls"].forEach(function (a, i) {
                lines.push("export NTFY_AUTH_ACCESS_" + i + '_USER="' + (a.user || "*") + '"');
                lines.push("export NTFY_AUTH_ACCESS_" + i + '_TOPIC="' + a.topic + '"');
                lines.push("export NTFY_AUTH_ACCESS_" + i + '_PERMISSION="' + a.permission + '"');
            });
        }

        if (values["_auth-tokens"]) {
            values["_auth-tokens"].forEach(function (t, i) {
                var q = t.token.indexOf("$") !== -1 ? "'" : '"';
                lines.push("export NTFY_AUTH_TOKENS_" + i + '_USER="' + t.user + '"');
                lines.push("export NTFY_AUTH_TOKENS_" + i + "_TOKEN=" + q + t.token + q);
                if (t.label) lines.push("export NTFY_AUTH_TOKENS_" + i + '_LABEL="' + t.label + '"');
            });
        }

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

        var output = "";
        var hasValues = false;
        for (var k in values) {
            if (values.hasOwnProperty(k)) { hasValues = true; break; }
        }

        if (!hasValues) {
            codeEl.innerHTML = '<span class="cg-empty-msg">Configure options on the left to generate your config...</span>';
            return;
        }

        if (format === "server-yml") {
            output = generateServerYml(values);
        } else if (format === "docker-compose") {
            output = generateDockerCompose(values);
        } else {
            output = generateEnvVars(values);
        }

        codeEl.textContent = output;
    }

    function updateConditionalVisibility() {
        var gen = document.getElementById("config-generator-app");
        if (!gen) return;

        var isPostgres = gen.querySelector('input[name="cg-db-type"][value="postgres"]');
        isPostgres = isPostgres && isPostgres.checked;

        // Database fields
        var sqliteFields = gen.querySelector("#cg-sqlite-fields");
        var pgFields = gen.querySelector("#cg-postgres-fields");
        if (sqliteFields) sqliteFields.style.display = isPostgres ? "none" : "block";
        if (pgFields) pgFields.style.display = isPostgres ? "block" : "none";

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

        // Conditional sections (checkboxes that show/hide detail fields)
        var toggles = gen.querySelectorAll("[data-toggle]");
        toggles.forEach(function (toggle) {
            var target = gen.querySelector("#" + toggle.getAttribute("data-toggle"));
            if (target) {
                if (toggle.checked) {
                    target.classList.add("visible");
                } else {
                    target.classList.remove("visible");
                }
            }
        });
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

        // Accordion toggle
        gen.querySelectorAll(".cg-section-header").forEach(function (header) {
            header.addEventListener("click", function () {
                header.parentElement.classList.toggle("open");
            });
        });

        // Open first section by default
        var first = gen.querySelector(".cg-section");
        if (first) first.classList.add("open");

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
                updateConditionalVisibility();
                updateOutput();
            });
        });

        // Conditional toggles
        gen.querySelectorAll("[data-toggle]").forEach(function (toggle) {
            toggle.addEventListener("change", function () {
                updateConditionalVisibility();
                updateOutput();
            });
        });

        // Database radio
        gen.querySelectorAll('input[name="cg-db-type"]').forEach(function (r) {
            r.addEventListener("change", function () {
                updateConditionalVisibility();
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
            copyBtn.addEventListener("click", function () {
                var code = gen.querySelector("#cg-code");
                if (code && code.textContent) {
                    navigator.clipboard.writeText(code.textContent).then(function () {
                        copyBtn.textContent = "Copied!";
                        setTimeout(function () { copyBtn.textContent = "Copy"; }, 2000);
                    });
                }
            });
        }

        // Upstream checkbox special handling
        var upstreamCheck = gen.querySelector("#cg-upstream-check");
        if (upstreamCheck) {
            upstreamCheck.addEventListener("change", function () {
                var input = gen.querySelector('[data-key="upstream-base-url"]');
                if (input) input.value = upstreamCheck.checked ? "https://ntfy.sh" : "";
                updateOutput();
            });
        }

        // Metrics checkbox special handling
        var metricsCheck = gen.querySelector("#cg-metrics-check");
        if (metricsCheck) {
            metricsCheck.addEventListener("change", function () {
                var input = gen.querySelector('[data-key="enable-metrics"]');
                if (input) input.checked = metricsCheck.checked;
                updateOutput();
            });
        }

        updateConditionalVisibility();
        updateOutput();
    }

    if (document.readyState === "loading") {
        document.addEventListener("DOMContentLoaded", initGenerator);
    } else {
        initGenerator();
    }
})();
