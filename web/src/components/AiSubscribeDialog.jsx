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
import i18n from "i18next";
import aiApi from "../app/AiApi";
import { poller, subscribeTopic } from "../app/subscribe";
import DialogFooter from "./DialogFooter";
import { validTopic } from "../app/utils";

// applicableSubscriptions returns the plan's subscriptions that can actually be applied.
// The server validates topics too; this guards the Apply button against edit-tampering.
const applicableSubscriptions = (plan) => plan?.subscriptions?.filter((s) => validTopic(s.topic)) ?? [];

// AiSubscribeDialog is the axon subscription assistant: describe what you care about in
// natural language, review the generated plan, apply it with one click. The server never
// applies anything itself — this dialog subscribes through the exact same code path as
// the manual SubscribeDialog (subscribeTopic), so account sync, auth and dedup behave
// identically. Rendered only when config.enable_ai is set.
const AiSubscribeDialog = (props) => {
  const theme = useTheme();
  const fullScreen = useMediaQuery(theme.breakpoints.down("sm"));
  return (
    <Dialog open={props.open} onClose={props.onCancel} fullScreen={fullScreen}>
      <AiSubscribePage onCancel={props.onCancel} onSuccess={props.onSuccess} />
    </Dialog>
  );
};

export const AiSubscribePage = (props) => {
  const { t } = useTranslation();
  const [prompt, setPrompt] = useState("");
  const [planning, setPlanning] = useState(false);
  const [plan, setPlan] = useState(null);
  const [turns, setTurns] = useState([]); // Prior user/assistant turns for refinement
  const [refinement, setRefinement] = useState("");
  const [error, setError] = useState("");

  const planFromServer = async (wish, priorTurns) => {
    setPlanning(true);
    setError("");
    try {
      const generated = await aiApi.plan(wish, i18n.language || "en", priorTurns);
      setTurns([...priorTurns, { role: "user", content: wish }, { role: "assistant", content: JSON.stringify(generated) }]);
      setPlan(generated);
      setPrompt(wish); // Show the full conversation thread in the input for editing
      setRefinement("");
    } catch (e) {
      console.log(`[AiSubscribeDialog] Planning failed`, e);
      setError(e.message);
    } finally {
      setPlanning(false);
    }
  };

  const handlePlan = async () => planFromServer(prompt, turns);

  const handleRefine = async () => planFromServer(refinement, turns);

  const handleApply = async () => {
    const applicable = applicableSubscriptions(plan);
    if (applicable.length === 0) {
      return;
    }
    console.log(`[AiSubscribeDialog] Applying plan with ${applicable.length} subscription(s)`);
    const subscriptions = await Promise.all(
      applicable.map((sub) =>
        subscribeTopic(config.base_url, sub.topic, {
          displayName: sub.display_name,
          filter: sub.filters?.search,
        }),
      ),
    );
    const lastSubscription = subscriptions[subscriptions.length - 1];
    poller.pollInBackground(lastSubscription); // Dangle!
    props.onSuccess(lastSubscription);
  };

  const applicableCount = applicableSubscriptions(plan).length;

  return (
    <>
      <DialogTitle>{t("ai_subscribe_title")}</DialogTitle>
      <DialogContent>
        <DialogContentText>{t("ai_subscribe_description")}</DialogContentText>
        <TextField
          autoFocus
          margin="dense"
          id="ai-prompt"
          multiline
          minRows={3}
          maxRows={8}
          placeholder={t("ai_subscribe_placeholder")}
          value={prompt}
          onChange={(ev) => setPrompt(ev.target.value)}
          fullWidth
          variant="standard"
          disabled={planning || plan != null}
          slotProps={{
            htmlInput: { maxLength: 1000, "aria-label": t("ai_subscribe_placeholder") },
          }}
        />
        {plan && <PlanReview plan={plan} />}
        {plan && (
          <div style={{ display: "flex", gap: "8px", alignItems: "center", marginTop: "8px" }}>
            <TextField
              margin="dense"
              placeholder={t("ai_refine_placeholder")}
              value={refinement}
              onChange={(ev) => setRefinement(ev.target.value)}
              fullWidth
              variant="standard"
              disabled={planning}
              slotProps={{ htmlInput: { maxLength: 1000, "aria-label": t("ai_refine_placeholder") } }}
            />
            <Button onClick={handleRefine} disabled={planning || refinement.trim().length === 0}>
              {t("ai_refine_button")}
            </Button>
          </div>
        )}
      </DialogContent>
      <DialogFooter status={error}>
        <Button onClick={props.onCancel} disabled={planning}>
          {t("ai_subscribe_button_cancel")}
        </Button>
        {!plan && (
          <Button onClick={handlePlan} disabled={planning || prompt.trim().length === 0}>
            {planning && <CircularProgress size={18} sx={{ marginRight: 1 }} />}
            {t("ai_subscribe_button_plan")}
          </Button>
        )}
        {plan && (
          <>
            <Button onClick={() => setPlan(null)} disabled={planning}>
              {t("ai_subscribe_button_back")}
            </Button>
            <Button onClick={handleApply} disabled={applicableCount === 0}>
              {t("ai_subscribe_button_apply", { count: applicableCount })}
            </Button>
          </>
        )}
      </DialogFooter>
    </>
  );
};

// PlanReview shows the generated plan for review. Justification and disclaimer are
// server-sanitized display text; the user applies (or discards) the whole plan.
const PlanReview = ({ plan }) => {
  const { t } = useTranslation();
  return (
    <>
      {plan.subscriptions.map((sub, i) => (
        <PlanSubscriptionCard key={sub.topic} subscription={sub} />
      ))}
      {plan.publisher_instructions && (
        <>
          <Typography variant="body2" sx={{ mt: 2 }}>
            {t("ai_subscribe_publisher_instructions")}
          </Typography>
          <pre style={{ whiteSpace: "pre-wrap", background: "#f5f5f5", padding: "12px", borderRadius: "4px", fontSize: "0.8rem" }}>
            {plan.publisher_instructions}
          </pre>
        </>
      )}
      <Alert severity="info" sx={{ mt: 2 }}>
        {plan.disclaimer}
      </Alert>
    </>
  );
};

const PlanSubscriptionCard = ({ subscription }) => {
  const { t } = useTranslation();
  return (
    <div style={{ marginTop: "16px" }}>
      <Typography variant="subtitle2">{subscription.display_name || subscription.topic}</Typography>
      <Typography variant="body2" color="text.secondary">
        {t("ai_subscribe_topic_label")}: {subscription.topic}
        {subscription.filters?.search ? ` · ${t("ai_subscribe_filter_label")}: ${subscription.filters.search}` : ""}
        {subscription.filters?.min_priority ? ` · ${t("ai_subscribe_priority_label")}: ≥ ${subscription.filters.min_priority}` : ""}
      </Typography>
      {subscription.justification && (
        <Typography variant="body2" color="text.secondary" sx={{ mt: 0.5 }}>
          {subscription.justification}
        </Typography>
      )}
    </div>
  );
};

export default AiSubscribeDialog;
