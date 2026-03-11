// Config Generator for ntfy
(function () {
    "use strict";

    var CONFIG = [
        { key: "base-url", env: "NTFY_BASE_URL", section: "basic" },
        { key: "behind-proxy", env: "NTFY_BEHIND_PROXY", section: "basic", type: "bool" },
        { key: "database-url", env: "NTFY_DATABASE_URL", section: "database" },
        { key: "auth-file", env: "NTFY_AUTH_FILE", section: "auth" },
        { key: "auth-default-access", env: "NTFY_AUTH_DEFAULT_ACCESS", section: "auth", def: "read-write" },
        { key: "enable-login", env: "NTFY_ENABLE_LOGIN", section: "auth", type: "bool" },
        { key: "enable-signup", env: "NTFY_ENABLE_SIGNUP", section: "auth", type: "bool" },
        { key: "attachment-cache-dir", env: "NTFY_ATTACHMENT_CACHE_DIR", section: "attach" },
        { key: "attachment-file-size-limit", env: "NTFY_ATTACHMENT_FILE_SIZE_LIMIT", section: "attach", def: "15M" },
        { key: "attachment-total-size-limit", env: "NTFY_ATTACHMENT_TOTAL_SIZE_LIMIT", section: "attach", def: "5G" },
        { key: "attachment-expiry-duration", env: "NTFY_ATTACHMENT_EXPIRY_DURATION", section: "attach", def: "3h" },
        { key: "cache-file", env: "NTFY_CACHE_FILE", section: "cache" },
        { key: "cache-duration", env: "NTFY_CACHE_DURATION", section: "cache", def: "12h" },
        { key: "web-push-public-key", env: "NTFY_WEB_PUSH_PUBLIC_KEY", section: "webpush" },
        { key: "web-push-private-key", env: "NTFY_WEB_PUSH_PRIVATE_KEY", section: "webpush" },
        { key: "web-push-file", env: "NTFY_WEB_PUSH_FILE", section: "webpush" },
        { key: "web-push-email-address", env: "NTFY_WEB_PUSH_EMAIL_ADDRESS", section: "webpush" },
        { key: "smtp-sender-addr", env: "NTFY_SMTP_SENDER_ADDR", section: "smtp-out" },
        { key: "smtp-sender-from", env: "NTFY_SMTP_SENDER_FROM", section: "smtp-out" },
        { key: "smtp-sender-user", env: "NTFY_SMTP_SENDER_USER", section: "smtp-out" },
        { key: "smtp-sender-pass", env: "NTFY_SMTP_SENDER_PASS", section: "smtp-out" },
        { key: "smtp-server-listen", env: "NTFY_SMTP_SERVER_LISTEN", section: "smtp-in" },
        { key: "smtp-server-domain", env: "NTFY_SMTP_SERVER_DOMAIN", section: "smtp-in" },
        { key: "smtp-server-addr-prefix", env: "NTFY_SMTP_SERVER_ADDR_PREFIX", section: "smtp-in" },
        { key: "upstream-base-url", env: "NTFY_UPSTREAM_BASE_URL", section: "upstream" },
    ];

    // Feature checkbox → nav tab ID
    var NAV_MAP = {
        "cg-feat-auth": "cg-nav-auth",
        "cg-feat-cache": "cg-nav-cache",
        "cg-feat-attach": "cg-nav-attach",
        "cg-feat-webpush": "cg-nav-webpush",
    };

    function collectValues() {
        var values = {};
        var modal = document.getElementById("cg-modal");
        if (!modal) return values;

        CONFIG.forEach(function (c) {
            var el = modal.querySelector('[data-key="' + c.key + '"]');
            if (!el) return;

            // Skip fields in hidden panels (feature not enabled)
            var panel = el.closest(".cg-panel");
            if (panel) {
                // Panel hidden directly (e.g. PostgreSQL panel when SQLite selected)
                if (panel.style.display === "none") return;
                // Panel with a nav tab that is hidden (feature not enabled)
                if (!panel.classList.contains("active")) {
                    var panelId = panel.id;
                    var navTab = modal.querySelector('[data-panel="' + panelId + '"]');
                    if (!navTab || navTab.style.display === "none") return;
                }
            }

            // Skip hidden individual fields or sections
            var ancestor = el.parentElement;
            while (ancestor && ancestor !== modal) {
                if (ancestor.style.display === "none") return;
                ancestor = ancestor.parentElement;
            }

            var val;
            if (c.type === "bool") {
                if (el.checked) val = "true";
            } else {
                val = el.value.trim();
                if (!val) return;
            }
            if (val && c.def && val === c.def) return;
            if (val) values[c.key] = val;
        });

        // Provisioned users
        var userRows = modal.querySelectorAll(".cg-auth-user-row");
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
        var aclRows = modal.querySelectorAll(".cg-auth-acl-row");
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
        var tokenRows = modal.querySelectorAll(".cg-auth-token-row");
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

        // UnifiedPush ACL
        var upYes = modal.querySelector('input[name="cg-unifiedpush"][value="yes"]');
        if (upYes && upYes.checked) {
            if (!values["_auth-acls"]) values["_auth-acls"] = [];
            values["_auth-acls"].unshift({ user: "*", topic: "up*", permission: "write-only" });
        }

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
        };
        var lastSection = "";
        var hadAuth = false;

        CONFIG.forEach(function (c) {
            if (!(c.key in values)) return;
            if (c.section !== lastSection) {
                if (lines.length) lines.push("");
                if (sections[c.section]) lines.push(sections[c.section]);
                lastSection = c.section;
            }
            if (c.section === "auth") hadAuth = true;
            var val = values[c.key];
            if (c.type === "bool") {
                lines.push(c.key + ": true");
            } else {
                lines.push(c.key + ': "' + val + '"');
            }
        });

        // Find where auth section ends to insert users/acls/tokens there
        var authInsertIdx = lines.length;
        if (hadAuth) {
            for (var i = 0; i < lines.length; i++) {
                if (lines[i] === "# Access control") {
                    // Find the end of this section (next section comment or end)
                    for (var j = i + 1; j < lines.length; j++) {
                        if (lines[j].indexOf("# ") === 0) { authInsertIdx = j - 1; break; }
                        authInsertIdx = j + 1;
                    }
                    break;
                }
            }
        }

        var authExtra = [];
        if (values["_auth-users"]) {
            if (!hadAuth) { authExtra.push(""); authExtra.push("# Access control"); hadAuth = true; }
            authExtra.push("auth-users:");
            values["_auth-users"].forEach(function (u) {
                authExtra.push('  - "' + u.username + ":" + u.password + ":" + u.role + '"');
            });
        }

        if (values["_auth-acls"]) {
            if (!hadAuth) { authExtra.push(""); authExtra.push("# Access control"); hadAuth = true; }
            authExtra.push("auth-access:");
            values["_auth-acls"].forEach(function (a) {
                authExtra.push('  - "' + (a.user || "*") + ":" + a.topic + ":" + a.permission + '"');
            });
        }

        if (values["_auth-tokens"]) {
            if (!hadAuth) { authExtra.push(""); authExtra.push("# Access control"); hadAuth = true; }
            authExtra.push("auth-tokens:");
            values["_auth-tokens"].forEach(function (t) {
                var entry = t.user + ":" + t.token;
                if (t.label) entry += ":" + t.label;
                authExtra.push('  - "' + entry + '"');
            });
        }

        // Splice auth extras into the right position
        if (authExtra.length) {
            lines.splice.apply(lines, [authInsertIdx, 0].concat(authExtra));
        }

        return lines.join("\n");
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
            }
            if (val.indexOf("$") !== -1) {
                val = val.replace(/\$/g, "$$$$");
                lines.push("      # Note: $ is doubled to $$ for docker-compose");
            }
            lines.push("      " + c.env + ": " + val);
        });

        if (values["_auth-users"]) {
            var usersVal = values["_auth-users"].map(function (u) {
                return u.username + ":" + u.password + ":" + u.role;
            }).join(",");
            usersVal = usersVal.replace(/\$/g, "$$$$");
            lines.push("      # Note: $ is doubled to $$ for docker-compose");
            lines.push("      NTFY_AUTH_USERS: " + usersVal);
        }

        if (values["_auth-acls"]) {
            var aclsVal = values["_auth-acls"].map(function (a) {
                return (a.user || "*") + ":" + a.topic + ":" + a.permission;
            }).join(",");
            lines.push("      NTFY_AUTH_ACCESS: " + aclsVal);
        }

        if (values["_auth-tokens"]) {
            var tokensVal = values["_auth-tokens"].map(function (t) {
                var entry = t.user + ":" + t.token;
                if (t.label) entry += ":" + t.label;
                return entry;
            }).join(",");
            lines.push("      NTFY_AUTH_TOKENS: " + tokensVal);
        }

        lines.push("    volumes:");
        lines.push("      - /var/cache/ntfy:/var/cache/ntfy");
        lines.push("      - /etc/ntfy:/etc/ntfy");
        lines.push("    ports:");
        lines.push('      - "80:80"');
        lines.push("    restart: unless-stopped");

        return lines.join("\n");
    }

    function generateEnvVars(values) {
        var lines = [];

        CONFIG.forEach(function (c) {
            if (!(c.key in values)) return;
            var val = values[c.key];
            if (c.type === "bool") val = "true";
            var q = val.indexOf("$") !== -1 ? "'" : '"';
            lines.push(c.env + "=" + q + val + q);
        });

        if (values["_auth-users"]) {
            var usersStr = values["_auth-users"].map(function (u) {
                return u.username + ":" + u.password + ":" + u.role;
            }).join(",");
            var q = usersStr.indexOf("$") !== -1 ? "'" : '"';
            lines.push("NTFY_AUTH_USERS=" + q + usersStr + q);
        }

        if (values["_auth-acls"]) {
            var aclsStr = values["_auth-acls"].map(function (a) {
                return (a.user || "*") + ":" + a.topic + ":" + a.permission;
            }).join(",");
            lines.push('NTFY_AUTH_ACCESS="' + aclsStr + '"');
        }

        if (values["_auth-tokens"]) {
            var tokensStr = values["_auth-tokens"].map(function (t) {
                var entry = t.user + ":" + t.token;
                if (t.label) entry += ":" + t.label;
                return entry;
            }).join(",");
            lines.push('NTFY_AUTH_TOKENS="' + tokensStr + '"');
        }

        return lines.join("\n");
    }

    // Web Push VAPID key generation (P-256 ECDH)
    function generateVAPIDKeys() {
        return crypto.subtle.generateKey(
            { name: "ECDH", namedCurve: "P-256" },
            true,
            ["deriveBits"]
        ).then(function (keyPair) {
            return Promise.all([
                crypto.subtle.exportKey("raw", keyPair.publicKey),
                crypto.subtle.exportKey("pkcs8", keyPair.privateKey)
            ]);
        }).then(function (keys) {
            var pubBytes = new Uint8Array(keys[0]);
            var privPkcs8 = new Uint8Array(keys[1]);
            // Extract raw 32-byte private key from PKCS#8 (last 32 bytes of the DER)
            var privBytes = privPkcs8.slice(privPkcs8.length - 32);
            return {
                publicKey: arrayToBase64Url(pubBytes),
                privateKey: arrayToBase64Url(privBytes)
            };
        });
    }

    function arrayToBase64Url(arr) {
        var str = "";
        for (var i = 0; i < arr.length; i++) {
            str += String.fromCharCode(arr[i]);
        }
        return btoa(str).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
    }

    function updateOutput() {
        var modal = document.getElementById("cg-modal");
        if (!modal) return;

        var values = collectValues();
        var codeEl = modal.querySelector("#cg-code");
        if (!codeEl) return;

        var activeTab = modal.querySelector(".cg-output-tab.active");
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
        } else if (format === "env-vars") {
            output = generateEnvVars(values);
        } else {
            output = generateServerYml(values);
        }

        codeEl.textContent = output;

        // Validation warnings
        var warnings = validate(modal, values);
        var warningsEl = modal.querySelector("#cg-warnings");
        if (warningsEl) {
            if (warnings.length) {
                warningsEl.innerHTML = warnings.map(function (w) {
                    return '<div class="cg-warning">' + w + '</div>';
                }).join("");
                warningsEl.style.display = "";
            } else {
                warningsEl.style.display = "none";
            }
        }
    }

    function validate(modal, values) {
        var warnings = [];
        var baseUrl = values["base-url"] || "";

        // base-url format
        if (baseUrl) {
            if (baseUrl.indexOf("http://") !== 0 && baseUrl.indexOf("https://") !== 0) {
                warnings.push("base-url must start with http:// or https://");
            } else {
                try {
                    var u = new URL(baseUrl);
                    if (u.pathname !== "/" && u.pathname !== "") {
                        warnings.push("base-url must not have a path, ntfy does not support sub-paths");
                    }
                } catch (e) {
                    warnings.push("base-url is not a valid URL");
                }
            }
        }

        // Web push requires all fields + base-url
        var wpPublic = values["web-push-public-key"];
        var wpPrivate = values["web-push-private-key"];
        var wpEmail = values["web-push-email-address"];
        var wpFile = values["web-push-file"];
        var dbUrl = values["database-url"];
        if (wpPublic || wpPrivate || wpEmail) {
            var missing = [];
            if (!wpPublic) missing.push("web-push-public-key");
            if (!wpPrivate) missing.push("web-push-private-key");
            if (!wpFile && !dbUrl) missing.push("web-push-file or database-url");
            if (!wpEmail) missing.push("web-push-email-address");
            if (!baseUrl) missing.push("base-url");
            if (missing.length) {
                warnings.push("Web push requires: " + missing.join(", "));
            }
        }

        // SMTP sender requires base-url and smtp-sender-from
        if (values["smtp-sender-addr"]) {
            var smtpMissing = [];
            if (!baseUrl) smtpMissing.push("base-url");
            if (!values["smtp-sender-from"]) smtpMissing.push("smtp-sender-from");
            if (smtpMissing.length) {
                warnings.push("Email sending requires: " + smtpMissing.join(", "));
            }
        }

        // SMTP server requires domain
        if (values["smtp-server-listen"] && !values["smtp-server-domain"]) {
            warnings.push("Email publishing requires smtp-server-domain");
        }

        // Attachments require base-url
        if (values["attachment-cache-dir"] && !baseUrl) {
            warnings.push("Attachments require base-url to be set");
        }

        // Upstream requires base-url and can't equal it
        if (values["upstream-base-url"]) {
            if (!baseUrl) {
                warnings.push("Upstream server requires base-url to be set");
            } else if (baseUrl === values["upstream-base-url"]) {
                warnings.push("base-url and upstream-base-url cannot be the same");
            }
        }

        // enable-signup requires enable-login
        if (values["enable-signup"] && !values["enable-login"]) {
            warnings.push("Enable signup requires enable-login to also be set");
        }

        return warnings;
    }

    function prefill(modal, key, value) {
        var el = modal.querySelector('[data-key="' + key + '"]');
        if (el && !el.value.trim()) el.value = value;
    }


    function updateVisibility() {
        var modal = document.getElementById("cg-modal");
        if (!modal) return;

        var isPostgres = modal.querySelector('input[name="cg-db-type"][value="postgres"]');
        isPostgres = isPostgres && isPostgres.checked;

        var isPrivate = modal.querySelector('input[name="cg-server-type"][value="private"]');
        isPrivate = isPrivate && isPrivate.checked;

        var isUnifiedPush = modal.querySelector('input[name="cg-unifiedpush"][value="yes"]');
        isUnifiedPush = isUnifiedPush && isUnifiedPush.checked;

        // Auto-check auth when private or UnifiedPush is selected
        var authCheck = modal.querySelector("#cg-feat-auth");
        if (authCheck) {
            var authForced = isPrivate || isUnifiedPush || isPostgres;
            if (authForced) authCheck.checked = true;
            authCheck.disabled = authForced;
        }

        var authEnabled = authCheck && authCheck.checked;

        var cacheEnabled = modal.querySelector("#cg-feat-cache");
        cacheEnabled = cacheEnabled && cacheEnabled.checked;

        var attachEnabled = modal.querySelector("#cg-feat-attach");
        attachEnabled = attachEnabled && attachEnabled.checked;

        var webpushEnabled = modal.querySelector("#cg-feat-webpush");
        webpushEnabled = webpushEnabled && webpushEnabled.checked;

        var smtpOutEnabled = modal.querySelector("#cg-feat-smtp-out");
        smtpOutEnabled = smtpOutEnabled && smtpOutEnabled.checked;

        var smtpInEnabled = modal.querySelector("#cg-feat-smtp-in");
        smtpInEnabled = smtpInEnabled && smtpInEnabled.checked;

        // Show database question only if a DB-dependent feature is selected
        var needsDb = authEnabled || cacheEnabled || webpushEnabled;
        var dbStep = modal.querySelector("#cg-wizard-db");
        if (dbStep) dbStep.style.display = needsDb ? "" : "none";

        // Nav tabs for features
        for (var featId in NAV_MAP) {
            var checkbox = modal.querySelector("#" + featId);
            var navTab = modal.querySelector("#" + NAV_MAP[featId]);
            if (checkbox && navTab) {
                navTab.style.display = checkbox.checked ? "" : "none";
            }
        }

        // Email tab — show if either outgoing or incoming is enabled
        var navEmail = modal.querySelector("#cg-nav-email");
        if (navEmail) navEmail.style.display = (smtpOutEnabled || smtpInEnabled) ? "" : "none";
        var emailOutSection = modal.querySelector("#cg-email-out-section");
        if (emailOutSection) emailOutSection.style.display = smtpOutEnabled ? "" : "none";
        var emailInSection = modal.querySelector("#cg-email-in-section");
        if (emailInSection) emailInSection.style.display = smtpInEnabled ? "" : "none";

        // Show/hide configure buttons next to feature checkboxes
        modal.querySelectorAll(".cg-btn-configure").forEach(function (btn) {
            var row = btn.closest(".cg-feature-row");
            if (!row) return;
            var cb = row.querySelector('input[type="checkbox"]');
            btn.style.display = (cb && cb.checked) ? "" : "none";
        });

        // If active nav tab got hidden, switch to General
        var activeNav = modal.querySelector(".cg-nav-tab.active");
        if (activeNav && activeNav.style.display === "none") {
            switchPanel(modal, "cg-panel-general");
        }

        // Hide auth-file and web-push-file if PostgreSQL
        var authFile = modal.querySelector('[data-key="auth-file"]');
        if (authFile) {
            var authField = authFile.closest(".cg-field");
            if (authField) authField.style.display = isPostgres ? "none" : "";
        }
        var wpFile = modal.querySelector('[data-key="web-push-file"]');
        if (wpFile) {
            var wpField = wpFile.closest(".cg-field");
            if (wpField) wpField.style.display = isPostgres ? "none" : "";
        }

        // Hide cache-file when PostgreSQL
        var cacheFileField = modal.querySelector("#cg-cache-file-field");
        if (cacheFileField) cacheFileField.style.display = isPostgres ? "none" : "";

        // Database tab — show only when PostgreSQL is selected and a DB-dependent feature is on
        var navDb = modal.querySelector("#cg-nav-database");
        if (navDb) navDb.style.display = (needsDb && isPostgres) ? "" : "none";

        // iOS question → upstream-base-url
        var iosYes = modal.querySelector('input[name="cg-ios"][value="yes"]');
        var upstreamInput = modal.querySelector('[data-key="upstream-base-url"]');
        if (iosYes && upstreamInput) {
            upstreamInput.value = iosYes.checked ? "https://ntfy.sh" : "";
        }

        // Proxy radio → hidden checkbox
        var proxyYes = modal.querySelector('input[name="cg-proxy"][value="yes"]');
        var proxyCheckbox = modal.querySelector("#cg-behind-proxy");
        if (proxyYes && proxyCheckbox) {
            proxyCheckbox.checked = proxyYes.checked;
        }

        // Default access select → hidden input
        var accessSelect = modal.querySelector("#cg-default-access-select");
        var accessHidden = modal.querySelector('input[type="hidden"][data-key="auth-default-access"]');
        if (accessSelect && accessHidden) {
            accessHidden.value = accessSelect.value;
        }

        // Login/signup radios → hidden checkboxes
        var loginYes = modal.querySelector('input[name="cg-enable-login"][value="yes"]');
        var loginHidden = modal.querySelector("#cg-enable-login-hidden");
        if (loginYes && loginHidden) loginHidden.checked = loginYes.checked;

        var signupYes = modal.querySelector('input[name="cg-enable-signup"][value="yes"]');
        var signupHidden = modal.querySelector("#cg-enable-signup-hidden");
        if (signupYes && signupHidden) signupHidden.checked = signupYes.checked;

        // --- Pre-fill defaults ---
        if (isPostgres) {
            prefill(modal, "database-url", "postgres://user:pass@host:5432/ntfy");
        }

        if (authEnabled) {
            if (!isPostgres) prefill(modal, "auth-file", "/var/lib/ntfy/auth.db");
        }
        if (isPrivate) {
            // Set default access select to deny-all
            if (accessSelect) accessSelect.value = "deny-all";
            if (accessHidden) accessHidden.value = "deny-all";
            // Enable login
            var loginYesRadio = modal.querySelector('input[name="cg-enable-login"][value="yes"]');
            if (loginYesRadio) loginYesRadio.checked = true;
            if (loginHidden) loginHidden.checked = true;
        } else {
            // Open server: reset default access to read-write
            if (accessSelect) accessSelect.value = "read-write";
            if (accessHidden) accessHidden.value = "read-write";
        }

        if (cacheEnabled) {
            if (!isPostgres) prefill(modal, "cache-file", "/var/cache/ntfy/cache.db");
            prefill(modal, "cache-duration", "12h");
        }

        if (attachEnabled) {
            prefill(modal, "attachment-cache-dir", "/var/cache/ntfy/attachments");
            prefill(modal, "attachment-file-size-limit", "15M");
            prefill(modal, "attachment-total-size-limit", "5G");
            prefill(modal, "attachment-expiry-duration", "3h");
        }

        if (webpushEnabled) {
            if (!isPostgres) prefill(modal, "web-push-file", "/var/lib/ntfy/webpush.db");
            prefill(modal, "web-push-email-address", "admin@example.com");
        }

        if (smtpOutEnabled) {
            prefill(modal, "smtp-sender-addr", "smtp.example.com:587");
            prefill(modal, "smtp-sender-from", "ntfy@example.com");
        }

        if (smtpInEnabled) {
            prefill(modal, "smtp-server-listen", ":25");
            prefill(modal, "smtp-server-domain", "ntfy.example.com");
        }
    }

    function switchPanel(modal, panelId) {
        modal.querySelectorAll(".cg-nav-tab").forEach(function (t) { t.classList.remove("active"); });
        modal.querySelectorAll(".cg-panel").forEach(function (p) { p.classList.remove("active"); });

        var navTab = modal.querySelector('[data-panel="' + panelId + '"]');
        var panel = modal.querySelector("#" + panelId);
        if (navTab) navTab.classList.add("active");
        if (panel) panel.classList.add("active");
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
        var modal = document.getElementById("cg-modal");
        if (!modal) return;

        var openBtn = document.getElementById("cg-open-btn");
        var closeBtn = document.getElementById("cg-close-btn");
        var backdrop = modal.querySelector(".cg-modal-backdrop");

        function openModal() {
            modal.style.display = "";
            document.body.style.overflow = "hidden";
            updateVisibility();
            updateOutput();
        }

        function closeModal() {
            modal.style.display = "none";
            document.body.style.overflow = "";
        }

        var resetBtn = document.getElementById("cg-reset-btn");

        function resetAll() {
            // Reset all text/password inputs
            modal.querySelectorAll('input[type="text"], input[type="password"]').forEach(function (el) {
                el.value = "";
            });
            // Uncheck all checkboxes
            modal.querySelectorAll('input[type="checkbox"]').forEach(function (el) {
                el.checked = false;
                el.disabled = false;
            });
            // Reset radio buttons to first option
            var radioGroups = {};
            modal.querySelectorAll('input[type="radio"]').forEach(function (el) {
                if (!radioGroups[el.name]) {
                    radioGroups[el.name] = true;
                    var first = modal.querySelector('input[type="radio"][name="' + el.name + '"]');
                    if (first) first.checked = true;
                } else {
                    el.checked = false;
                }
            });
            // Reset selects to first option
            modal.querySelectorAll("select").forEach(function (el) {
                el.selectedIndex = 0;
            });
            // Remove all repeatable rows
            modal.querySelectorAll(".cg-auth-user-row, .cg-auth-acl-row, .cg-auth-token-row").forEach(function (row) {
                row.remove();
            });
            // Re-prefill base-url
            var baseUrlInput = modal.querySelector('[data-key="base-url"]');
            if (baseUrlInput) {
                var host = window.location.hostname;
                if (host && host.indexOf("ntfy.sh") === -1) {
                    baseUrlInput.value = "https://ntfy.example.com";
                }
            }
            // Reset to General tab
            switchPanel(modal, "cg-panel-general");
            updateVisibility();
            updateOutput();
        }

        if (openBtn) openBtn.addEventListener("click", openModal);
        if (closeBtn) closeBtn.addEventListener("click", closeModal);
        if (resetBtn) resetBtn.addEventListener("click", resetAll);
        if (backdrop) backdrop.addEventListener("click", closeModal);

        document.addEventListener("keydown", function (e) {
            if (e.key === "Escape" && modal.style.display !== "none") {
                closeModal();
            }
        });

        // Left nav tab switching
        modal.querySelectorAll(".cg-nav-tab").forEach(function (tab) {
            tab.addEventListener("click", function () {
                var panelId = tab.getAttribute("data-panel");
                switchPanel(modal, panelId);
            });
        });

        // Configure buttons in feature grid
        modal.querySelectorAll(".cg-btn-configure").forEach(function (btn) {
            btn.addEventListener("click", function () {
                var panelId = btn.getAttribute("data-panel");
                if (panelId) switchPanel(modal, panelId);
            });
        });

        // Output format tab switching
        modal.querySelectorAll(".cg-output-tab").forEach(function (tab) {
            tab.addEventListener("click", function () {
                modal.querySelectorAll(".cg-output-tab").forEach(function (t) { t.classList.remove("active"); });
                tab.classList.add("active");
                updateOutput();
            });
        });

        // All form inputs trigger update
        modal.querySelectorAll("input, select").forEach(function (el) {
            var evt = (el.type === "checkbox" || el.type === "radio") ? "change" : "input";
            el.addEventListener(evt, function () {
                updateVisibility();
                updateOutput();
            });
        });

        // Add buttons for repeatable rows
        modal.querySelectorAll(".cg-btn-add[data-add-type]").forEach(function (btn) {
            btn.addEventListener("click", function () {
                var type = btn.getAttribute("data-add-type");
                var container = btn.previousElementSibling;
                if (!container) container = btn.parentElement.querySelector(".cg-repeatable-container");
                addRepeatableRow(container, type);
            });
        });

        // Copy button
        var copyBtn = modal.querySelector("#cg-copy-btn");
        if (copyBtn) {
            var copyIcon = '<svg xmlns="http://www.w3.org/2000/svg" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="9" y="9" width="13" height="13" rx="2" ry="2"></rect><path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"></path></svg>';
            var checkIcon = '<svg xmlns="http://www.w3.org/2000/svg" width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polyline points="20 6 9 17 4 12"></polyline></svg>';
            copyBtn.addEventListener("click", function () {
                var code = modal.querySelector("#cg-code");
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

        // VAPID key generation for web push
        var vapidKeysGenerated = false;
        var regenBtn = modal.querySelector("#cg-regen-keys");

        function fillVAPIDKeys() {
            generateVAPIDKeys().then(function (keys) {
                var pubInput = modal.querySelector('[data-key="web-push-public-key"]');
                var privInput = modal.querySelector('[data-key="web-push-private-key"]');
                if (pubInput) pubInput.value = keys.publicKey;
                if (privInput) privInput.value = keys.privateKey;
                updateOutput();
            });
        }

        if (regenBtn) {
            regenBtn.addEventListener("click", function () {
                fillVAPIDKeys();
            });
        }

        // Auto-generate keys when web push is first enabled
        var webpushFeat = modal.querySelector("#cg-feat-webpush");
        if (webpushFeat) {
            webpushFeat.addEventListener("change", function () {
                if (webpushFeat.checked && !vapidKeysGenerated) {
                    vapidKeysGenerated = true;
                    fillVAPIDKeys();
                }
            });
        }

        // Pre-fill base-url if not on ntfy.sh
        var baseUrlInput = modal.querySelector('[data-key="base-url"]');
        if (baseUrlInput && !baseUrlInput.value.trim()) {
            var host = window.location.hostname;
            if (host && host.indexOf("ntfy.sh") === -1) {
                baseUrlInput.value = "https://ntfy.example.com";
            }
        }

        // Auto-open if URL hash points to config generator
        if (window.location.hash === "#config-generator") {
            openModal();
        }
    }

    if (document.readyState === "loading") {
        document.addEventListener("DOMContentLoaded", initGenerator);
    } else {
        initGenerator();
    }
})();
