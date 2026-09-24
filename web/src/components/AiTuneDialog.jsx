import * as React from "react";
import { useState } from "react";
import {
  Alert,
  Button,
  CircularProgress,
  Dialog,
  DialogContent,
  DialogContentText,
  DialogTitle,
  TextField,
  Typography,
  useMediaQuery,
  useTheme,
} from "@mui/material";
import { useTranslation } from "react-i18next";
import aiApi from "../app/AiApi";
import subscriptionManager from "../app/SubscriptionManager";
import accountApi from "../app/AccountApi";
import session from "../app/Session";
import routes from "./routes";
import DialogFooter from "./DialogFooter";
import { UnauthorizedError } from "../app/errors";

// AiTuneDialog asks the axon planner to adjust an existing subscription ("quieter",
// "only critical at night") and shows the proposed settings for review before applying.
// The filter is stored on the local subscription row and sent to the server as a query
// filter (?q=); the display name additionally syncs to the user's account.
const AiTuneDialog = (props) => {
  const theme = useTheme();
  const fullScreen = useMediaQuery(theme.breakpoints.down("sm"));
  return (
    <Dialog open={props.open} onClose={props.onClose} maxWidth="sm" fullWidth fullScreen={fullScreen}>
      <AiTunePage subscription={props.subscription} onClose={props.onClose} />
    </Dialog>
  );
};

const AiTunePage = (props) => {
  const { t } = useTranslation();
  const { subscription } = props;
  const [goal, setGoal] = useState("");
  const [planning, setPlanning] = useState(false);
  const [result, setResult] = useState(null);
  const [displayName, setDisplayName] = useState("");
  const [search, setSearch] = useState("");
  const [minPriority, setMinPriority] = useState(0);
  const [error, setError] = useState("");

  const handlePlan = async () => {
    setPlanning(true);
    setError("");
    try {
      const tune = await aiApi.tune(
        subscription.topic,
        {
          displayName: subscription.displayName,
          search: subscription.filter,
          minPriority: subscription.minPriority ?? 0,
        },
        goal,
      );
      setResult(tune);
      setDisplayName(tune.display_name ?? subscription.displayName ?? "");
      setSearch(tune.filters?.search ?? "");
      setMinPriority(tune.filters?.min_priority ?? 0);
    } catch (e) {
      console.log(`[AiTuneDialog] Tuning failed`, e);
      setError(e.message);
    } finally {
      setPlanning(false);
    }
  };

  const handleApply = async () => {
    const filter = search.trim();
    const minPriorityParsed = Number.parseInt(minPriority, 10);
    const opts = {
      displayName: displayName.trim() || undefined,
      filter: filter || undefined,
      minPriority: Number.isFinite(minPriorityParsed) && minPriorityParsed >= 1 && minPriorityParsed <= 5 ? minPriorityParsed : undefined,
    };
    console.log(`[AiTuneDialog] Applying tuned settings to ${subscription.id}`, opts);
    await subscriptionManager.upsert(subscription.baseUrl, subscription.topic, opts);
    if (session.exists() && !subscription.internal) {
      try {
        await accountApi.updateSubscription(subscription.baseUrl, subscription.topic, { display_name: opts.displayName ?? "" });
      } catch (e) {
        console.log(`[AiTuneDialog] Error updating subscription`, e);
        if (e instanceof UnauthorizedError) {
          await session.resetAndRedirect(routes.login);
          return;
        }
      }
    }
    props.onClose();
  };

  const clampPriority = (value) => {
    const parsed = Number.parseInt(value, 10);
    if (!Number.isFinite(parsed)) {
      return "";
    }
    return Math.min(5, Math.max(0, parsed));
  };

  return (
    <>
      <DialogTitle>{t("ai_tune_title")}</DialogTitle>
      <DialogContent>
        <DialogContentText>{t("ai_tune_description", { topic: subscription.topic })}</DialogContentText>
        <TextField
          autoFocus
          margin="dense"
          id="ai-tune-goal"
          multiline
          minRows={2}
          maxRows={6}
          placeholder={t("ai_tune_placeholder")}
          value={goal}
          onChange={(ev) => setGoal(ev.target.value)}
          fullWidth
          variant="standard"
          disabled={planning || result != null}
          slotProps={{
            htmlInput: { maxLength: 1000, "aria-label": t("ai_tune_placeholder") },
          }}
        />
        {result && (
          <>
            {result.justification && (
              <Alert severity="info" sx={{ mt: 2 }}>
                {result.justification}
              </Alert>
            )}
            <TextField
              key="ai-tune-display-name"
              margin="dense"
              label={t("display_name_dialog_title")}
              value={displayName}
              onChange={(ev) => setDisplayName(ev.target.value)}
              fullWidth
              variant="standard"
              slotProps={{ htmlInput: { maxLength: 64 } }}
            />
            <TextField
              key="ai-tune-search"
              margin="dense"
              label={t("ai_tune_filter_label")}
              value={search}
              onChange={(ev) => setSearch(ev.target.value)}
              fullWidth
              variant="standard"
              helperText={t("ai_tune_filter_help")}
            />
            <TextField
              key="ai-tune-priority"
              margin="dense"
              label={t("ai_tune_priority_label")}
              value={minPriority}
              onChange={(ev) => setMinPriority(clampPriority(ev.target.value))}
              fullWidth
              variant="standard"
              helperText={t("ai_tune_priority_help")}
              type="number"
            />
          </>
        )}
      </DialogContent>
      <DialogFooter status={error}>
        <Button onClick={props.onClose} disabled={planning}>
          {t("common_cancel")}
        </Button>
        {!result && (
          <Button onClick={handlePlan} disabled={planning || goal.trim().length === 0}>
            {planning && <CircularProgress size={18} sx={{ marginRight: 1 }} />}
            {t("ai_tune_button_suggest")}
          </Button>
        )}
        {result != null && (
          <>
            <Button onClick={() => setResult(null)}>{t("ai_tune_button_back")}</Button>
            <Button onClick={handleApply}>{t("ai_tune_button_apply")}</Button>
          </>
        )}
      </DialogFooter>
    </>
  );
};

export default AiTuneDialog;
