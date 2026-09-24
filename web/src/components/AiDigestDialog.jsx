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

// AiDigestDialog summarizes the recent messages of one subscription with the server's
// AI layer ("what did I miss?"). Requires a logged-in user: only synced subscriptions
// can be digested (server-side rule).
const RANGES = [
  { value: "24h", labelKey: "ai_digest_range_24h" },
  { value: "168h", labelKey: "ai_digest_range_7d" },
  { value: "720h", labelKey: "ai_digest_range_30d" },
];

const AiDigestDialog = (props) => {
  const theme = useTheme();
  const fullScreen = useMediaQuery(theme.breakpoints.down("sm"));
  return (
    <Dialog open={props.open} onClose={props.onClose} maxWidth="sm" fullWidth fullScreen={fullScreen}>
      <AiDigestPage subscription={props.subscription} onClose={props.onClose} />
    </Dialog>
  );
};

const AiDigestPage = (props) => {
  const { t } = useTranslation();
  const { subscription } = props;
  const [since, setSince] = useState("24h");
  const [loading, setLoading] = useState(false);
  const [digest, setDigest] = useState(null);
  const [error, setError] = useState("");

  const handleDigest = async () => {
    setLoading(true);
    setError("");
    try {
      const result = await aiApi.digest(subscription.topic, since);
      setDigest(result);
    } catch (e) {
      console.log(`[AiDigestDialog] Digest failed`, e);
      setError(e.message);
    } finally {
      setLoading(false);
    }
  };

  return (
    <>
      <DialogTitle>{t("ai_digest_title")}</DialogTitle>
      <DialogContent>
        <DialogContentText>{t("ai_digest_description", { topic: subscription.topic })}</DialogContentText>
        <TextField
          select
          margin="dense"
          label={t("ai_digest_range_label")}
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
        {digest && (
          <div style={{ marginTop: "16px" }}>
            <Typography variant="subtitle1">{digest.headline}</Typography>
            {digest.sections?.map((section) => (
              <div key={section.title} style={{ marginTop: "12px" }}>
                <Typography variant="subtitle2">{section.title}</Typography>
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
              {digest.disclaimer} ({digest.message_count} messages)
            </Typography>
          </div>
        )}
      </DialogContent>
      <DialogFooter status={error}>
        <Button onClick={props.onClose} disabled={loading}>
          {t("common_cancel")}
        </Button>
        <Button onClick={handleDigest} disabled={loading}>
          {loading && <CircularProgress size={18} sx={{ marginRight: 1 }} />}
          {digest ? t("ai_digest_button_again") : t("ai_digest_button")}
        </Button>
      </DialogFooter>
    </>
  );
};

export default AiDigestDialog;
