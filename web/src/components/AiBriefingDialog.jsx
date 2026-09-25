import * as React from "react";
import { useState } from "react";
import {
  Button,
  CircularProgress,
  Dialog,
  DialogContent,
  DialogContentText,
  DialogTitle,
  MenuItem,
  TextField,
  Typography,
  useMediaQuery,
  useTheme,
} from "@mui/material";
import { useTranslation } from "react-i18next";
import aiApi from "../app/AiApi";
import DialogFooter from "./DialogFooter";

// AiBriefingDialog summarizes the recent messages across ALL of the user's topics on
// this server ("what did I miss?"). Requires a logged-in user. Opened from the action
// bar on the main screen (axon).
const RANGES = [
  { value: "24h", labelKey: "ai_digest_range_24h" },
  { value: "168h", labelKey: "ai_digest_range_7d" },
  { value: "720h", labelKey: "ai_digest_range_30d" },
];

const AiBriefingDialog = (props) => {
  const theme = useTheme();
  const fullScreen = useMediaQuery(theme.breakpoints.down("sm"));
  return (
    <Dialog open={props.open} onClose={props.onClose} maxWidth="sm" fullWidth fullScreen={fullScreen}>
      <AiBriefingPage onClose={props.onClose} />
    </Dialog>
  );
};

const AiBriefingPage = (props) => {
  const { t } = useTranslation();
  const [since, setSince] = useState("24h");
  const [loading, setLoading] = useState(false);
  const [briefing, setBriefing] = useState(null);
  const [error, setError] = useState("");

  const handleBrief = async () => {
    setLoading(true);
    setError("");
    try {
      const result = await aiApi.briefing(since);
      setBriefing(result);
    } catch (e) {
      console.log(`[AiBriefingDialog] Briefing failed`, e);
      setError(e.message);
    } finally {
      setLoading(false);
    }
  };

  return (
    <>
      <DialogTitle>{t("ai_briefing_title")}</DialogTitle>
      <DialogContent>
        <DialogContentText>{t("ai_briefing_description")}</DialogContentText>
        <TextField
          select
          margin="dense"
          label={t("ai_briefing_range_label")}
          value={since}
          onChange={(ev) => setSince(ev.target.value)}
          fullWidth
          variant="standard"
          disabled={loading}
        >
          {RANGES.map((range) => (
            <MenuItem key={range.value} value={range.value}>
              {t(range.labelKey)}
            </MenuItem>
          ))}
        </TextField>
        {briefing && (
          <div style={{ marginTop: "16px" }}>
            <Typography variant="subtitle1">{briefing.headline}</Typography>
            {briefing.sections?.map((section) => (
              <div key={`${section.topic}-${section.title}`} style={{ marginTop: "12px" }}>
                <Typography variant="subtitle2">
                  {section.topic && section.topic !== "_" ? `${section.topic}: ` : ""}
                  {section.title}
                </Typography>
                <ul style={{ margin: "4px 0 0 0", paddingLeft: "20px" }}>
                  {section.points?.map((point) => (
                    <li key={point}>
                      <Typography variant="body2">{point}</Typography>
                    </li>
                  ))}
                </ul>
              </div>
            ))}
            <Typography variant="body2" color="text.secondary" sx={{ mt: 2 }}>
              {briefing.disclaimer} ({briefing.message_count} messages in {briefing.topic_count} topics)
            </Typography>
          </div>
        )}
      </DialogContent>
      <DialogFooter status={error}>
        <Button onClick={props.onClose}>{t("common_cancel")}</Button>
        <Button onClick={handleBrief} disabled={loading}>
          {loading && <CircularProgress size={18} sx={{ marginRight: 1 }} />}
          {briefing ? t("ai_briefing_button_again") : t("ai_briefing_button")}
        </Button>
      </DialogFooter>
    </>
  );
};

export default AiBriefingDialog;
