import * as React from "react";
import { useEffect, useRef, useState } from "react";
import { Alert, Button, CircularProgress, DialogActions, DialogContent, DialogContentText, DialogTitle, Typography } from "@mui/material";
import { useTranslation } from "react-i18next";
import aiApi from "../app/AiApi";
import { AIProviderChangedError } from "../app/errors";

export const AiProviderDisclosure = ({ status }) => {
  const { t } = useTranslation();
  return (
    <>
      <Typography sx={{ mt: 1 }}>{t("ai_consent_provider", { provider: status.provider })}</Typography>
      {status.endpoint && (
        <Typography sx={{ overflowWrap: "anywhere" }}>{t("ai_consent_endpoint", { endpoint: status.endpoint })}</Typography>
      )}
      <Typography sx={{ mb: 2, overflowWrap: "anywhere" }}>{t("ai_consent_models", { models: status.models.join(", ") })}</Typography>
      <DialogContentText sx={{ mb: 2 }}>{t("ai_consent_data")}</DialogContentText>
      <DialogContentText sx={{ mb: 2 }}>{t("ai_consent_retention")}</DialogContentText>
      <DialogContentText>{t("ai_consent_scope")}</DialogContentText>
    </>
  );
};

// Feature children mount only after checking the account's current consent.
const AiConsentGate = ({ active, onClose, onConsent, children }) => {
  const { t } = useTranslation();
  const [status, setStatus] = useState(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [reload, setReload] = useState(0);
  const mounted = useRef(false);
  const generation = useRef(0);

  useEffect(() => {
    generation.current += 1;
    mounted.current = active;
    if (!active) {
      return undefined;
    }
    const controller = new AbortController();
    setStatus(null);
    setBusy(false);
    setError("");
    aiApi
      .consent({ signal: controller.signal })
      .then((value) => {
        if (!controller.signal.aborted) {
          setStatus(value);
        }
      })
      .catch((e) => {
        if (!controller.signal.aborted) {
          setError(e.message);
        }
      });
    return () => {
      mounted.current = false;
      generation.current += 1;
      controller.abort();
    };
  }, [active, reload]);

  const accept = async () => {
    const requestGeneration = generation.current;
    const isCurrent = () => mounted.current && generation.current === requestGeneration;
    setBusy(true);
    setError("");
    try {
      const accepted = await aiApi.grantConsent(status.configuration);
      if (isCurrent()) {
        setStatus(accepted);
        onConsent?.(accepted);
      }
    } catch (e) {
      if (isCurrent()) {
        setError(e.message);
        // Refresh the disclosure, never automatically accept a different provider.
        if (e instanceof AIProviderChangedError) {
          setStatus(null);
          try {
            const updated = await aiApi.consent();
            if (isCurrent()) {
              setStatus(updated);
            }
          } catch (refreshError) {
            if (isCurrent()) {
              setError(refreshError.message);
            }
          }
        }
      }
    } finally {
      if (isCurrent()) {
        setBusy(false);
      }
    }
  };

  if (!active) {
    return null;
  }
  if (status?.consented) {
    return children;
  }
  return (
    <>
      <DialogTitle>{t("ai_consent_title")}</DialogTitle>
      <DialogContent>
        {!status && !error && <CircularProgress aria-label={t("ai_consent_loading")} size={24} />}
        {status && <AiProviderDisclosure status={status} />}
        {status && !status.can_manage && (
          <Alert severity="info" sx={{ mt: 2 }}>
            {t("ai_consent_owner_required")}
          </Alert>
        )}
        {error && (
          <Alert severity="error" sx={{ mt: 2 }}>
            {error}
          </Alert>
        )}
      </DialogContent>
      <DialogActions>
        <Button onClick={onClose}>{t("ai_consent_cancel")}</Button>
        {!status && error && <Button onClick={() => setReload((value) => value + 1)}>{t("ai_consent_retry")}</Button>}
        <Button onClick={accept} disabled={!status?.can_manage || busy}>
          {busy && <CircularProgress size={18} sx={{ mr: 1 }} />}
          {t("ai_consent_accept")}
        </Button>
      </DialogActions>
    </>
  );
};

export default AiConsentGate;
