import * as React from "react";
import { useEffect, useState } from "react";
import { Alert, Button, Card, Dialog, Typography } from "@mui/material";
import { useTranslation } from "react-i18next";
import aiApi from "../app/AiApi";
import AiConsentGate, { AiProviderDisclosure } from "./AiConsentGate";

const AiPrivacySettings = () => {
  const { t } = useTranslation();
  const [status, setStatus] = useState(null);
  const [open, setOpen] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  useEffect(() => {
    const controller = new AbortController();
    aiApi.consent({ signal: controller.signal }).then((value) => {
      if (!controller.signal.aborted) {
        setStatus(value);
      }
    }).catch((e) => {
      if (!controller.signal.aborted) {
        setError(e.message);
      }
    });
    return () => controller.abort();
  }, []);

  const revoke = async () => {
    setBusy(true);
    setError("");
    try {
      setStatus(await aiApi.revokeConsent());
    } catch (e) {
      setError(e.message);
    } finally {
      setBusy(false);
    }
  };

  return (
    <Card sx={{ p: 3 }} aria-label={t("ai_consent_settings_title")}>
      <Typography variant="h5" sx={{ mb: 2 }}>{t("ai_consent_settings_title")}</Typography>
      <Typography sx={{ mb: 2 }}>{t(status?.consented ? "ai_consent_enabled" : "ai_consent_disabled")}</Typography>
      {status && <AiProviderDisclosure status={status} />}
      {error && <Alert severity="error" sx={{ mt: 2 }}>{error}</Alert>}
      {status && !status.can_manage && <Alert severity="info" sx={{ mt: 2 }}>{t("ai_consent_owner_required")}</Alert>}
      <Button sx={{ mt: 2 }} disabled={!status?.can_manage || busy} onClick={status?.consented ? revoke : () => setOpen(true)}>
        {t(status?.consented ? "ai_consent_revoke" : "ai_consent_review")}
      </Button>
      <Dialog open={open} onClose={() => setOpen(false)} maxWidth="sm" fullWidth>
        <AiConsentGate
          active={open}
          onClose={() => setOpen(false)}
          onConsent={(value) => {
            setStatus(value);
            setOpen(false);
          }}
        />
      </Dialog>
    </Card>
  );
};

export default AiPrivacySettings;
