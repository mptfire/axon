import * as React from "react";
import { useState } from "react";
import {
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
import DialogFooter from "./DialogFooter";

// AiChatDialog answers questions about one subscription's recent history ("what failed
// last night?"). Answers cite the cached messages they rely on; citations link to the
// message when it still exists locally.
const AiChatDialog = (props) => {
  const theme = useTheme();
  const fullScreen = useMediaQuery(theme.breakpoints.down("sm"));
  return (
    <Dialog open={props.open} onClose={props.onClose} maxWidth="sm" fullWidth fullScreen={fullScreen}>
      <AiChatPage subscription={props.subscription} onClose={props.onClose} />
    </Dialog>
  );
};

const AiChatPage = (props) => {
  const { t } = useTranslation();
  const { subscription } = props;
  const [question, setQuestion] = useState("");
  const [loading, setLoading] = useState(false);
  const [answer, setAnswer] = useState(null);
  const [error, setError] = useState("");

  const handleAsk = async () => {
    setLoading(true);
    setError("");
    try {
      const result = await aiApi.chat(subscription.topic, question);
      setAnswer(result);
      setQuestion("");
    } catch (e) {
      console.log(`[AiChatDialog] Chat failed`, e);
      setError(e.message);
    } finally {
      setLoading(false);
    }
  };

  return (
    <>
      <DialogTitle>{t("ai_chat_title")}</DialogTitle>
      <DialogContent>
        <DialogContentText>{t("ai_chat_description", { topic: subscription.topic })}</DialogContentText>
        <TextField
          autoFocus
          margin="dense"
          id="ai-chat-question"
          placeholder={t("ai_chat_placeholder")}
          value={question}
          onChange={(ev) => setQuestion(ev.target.value)}
          onKeyDown={(ev) => {
            if (ev.key === "Enter" && !ev.shiftKey && question.trim() && !loading) {
              ev.preventDefault();
              handleAsk();
            }
          }}
          fullWidth
          variant="standard"
          disabled={loading}
          slotProps={{ htmlInput: { maxLength: 1000, "aria-label": t("ai_chat_placeholder") } }}
        />
        {answer && (
          <div style={{ marginTop: "16px" }}>
            <Typography variant="body1" sx={{ whiteSpace: "pre-wrap" }}>
              {answer.answer}
            </Typography>
            {answer.citations?.length > 0 && (
              <>
                <Typography variant="body2" sx={{ mt: 2 }}>
                  {t("ai_chat_citations_label")}
                </Typography>
                <ul style={{ margin: "4px 0 0 0", paddingLeft: "20px" }}>
                  {answer.citations.map((citation) => (
                    <Citation key={citation.id} citation={citation} />
                  ))}
                </ul>
              </>
            )}
            <Typography variant="body2" color="text.secondary" sx={{ mt: 2 }}>
              {t("ai_chat_disclaimer_prefix")} — {answer.disclaimer}
            </Typography>
          </div>
        )}
      </DialogContent>
      <DialogFooter status={error}>
        <Button onClick={props.onClose}>{t("common_cancel")}</Button>
        <Button onClick={handleAsk} disabled={loading || question.trim().length === 0}>
          {loading && <CircularProgress size={18} sx={{ marginRight: 1 }} />}
          {answer ? t("ai_chat_button_again") : t("ai_chat_button")}
        </Button>
      </DialogFooter>
    </>
  );
};

// Citation shows the cited message as a short preview (per-message deep links do not
// exist in the web app yet).
const Citation = ({ citation }) => {
  const preview = citation.title || citation.message || "";
  const truncated = preview.length > 80 ? `${preview.slice(0, 80)}…` : preview;
  return (
    <li>
      <Typography variant="body2">{truncated}</Typography>
    </li>
  );
};

export default AiChatDialog;
