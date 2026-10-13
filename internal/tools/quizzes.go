package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/PejarRu/mcp-aules/internal/api"
)

type ListQuizzesInput struct {
	CourseID int `json:"course_id"`
}

type GetQuizStatusInput struct {
	QuizID int `json:"quiz_id"`
}

func HandleListQuizzes(ctx context.Context, client *api.Client, input ListQuizzesInput) (string, error) {
	if !client.IsAuthenticated() {
		return "", api.ErrNotAuthenticated
	}
	if input.CourseID == 0 {
		return "", fmt.Errorf("course_id is required")
	}
	data, err := client.Call(ctx, "mod_quiz_get_quizzes_by_courses", map[string]string{"courseids[0]": fmt.Sprint(input.CourseID)})
	if err != nil {
		return "", err
	}
	var response map[string]any
	if err := json.Unmarshal(data, &response); err != nil {
		return "", fmt.Errorf("parsing quizzes: %w", err)
	}
	quizzes := mapsValue(response["quizzes"])
	for _, quiz := range quizzes {
		intro := stringValue(quiz["intro"])
		quiz["intro_html"] = intro
		quiz["intro_text"] = stripHTML(intro)
		for _, key := range []string{"timeopen", "timeclose", "timemodified"} {
			quiz[key+"_rfc3339"] = rfc3339(int64Value(quiz[key]))
		}
	}
	return marshalResult(map[string]any{"course_id": input.CourseID, "timezone": "Europe/Madrid", "count": len(quizzes), "quizzes": quizzes})
}

func HandleGetQuizStatus(ctx context.Context, client *api.Client, input GetQuizStatusInput) (string, error) {
	if !client.IsAuthenticated() {
		return "", api.ErrNotAuthenticated
	}
	if input.QuizID == 0 {
		return "", fmt.Errorf("quiz_id is required")
	}
	courseIDs, err := getEnrolledCourseIDs(ctx, client)
	if err != nil {
		return "", err
	}
	params := map[string]string{}
	for i, id := range courseIDs {
		params[fmt.Sprintf("courseids[%d]", i)] = fmt.Sprint(id)
	}
	data, err := client.Call(ctx, "mod_quiz_get_quizzes_by_courses", params)
	if err != nil {
		return "", err
	}
	var discovery map[string]any
	if err := json.Unmarshal(data, &discovery); err != nil {
		return "", fmt.Errorf("parsing quizzes: %w", err)
	}
	var quiz map[string]any
	for _, candidate := range mapsValue(discovery["quizzes"]) {
		if intValue(candidate["id"]) == input.QuizID {
			quiz = candidate
			break
		}
	}
	if quiz == nil {
		return "", fmt.Errorf("quiz %d not found in enrolled courses", input.QuizID)
	}
	userID, err := ensureUserID(ctx, client)
	if err != nil {
		return "", err
	}
	attemptData, err := client.Call(ctx, "mod_quiz_get_user_attempts", map[string]string{"quizid": fmt.Sprint(input.QuizID), "userid": fmt.Sprint(userID), "status": "all", "includepreviews": "0"})
	if err != nil {
		return "", err
	}
	var attemptsResponse map[string]any
	if err := json.Unmarshal(attemptData, &attemptsResponse); err != nil {
		return "", fmt.Errorf("parsing quiz attempts: %w", err)
	}
	attempts := mapsValue(attemptsResponse["attempts"])
	for _, attempt := range attempts {
		for _, key := range []string{"timestart", "timefinish", "timemodified", "timecheckstate"} {
			attempt[key+"_rfc3339"] = rfc3339(int64Value(attempt[key]))
		}
	}
	bestData, err := client.Call(ctx, "mod_quiz_get_user_best_grade", map[string]string{"quizid": fmt.Sprint(input.QuizID), "userid": fmt.Sprint(userID)})
	if err != nil {
		return "", err
	}
	var best map[string]any
	if err := json.Unmarshal(bestData, &best); err != nil {
		return "", fmt.Errorf("parsing quiz best grade: %w", err)
	}
	metadata := map[string]any{"id": input.QuizID, "course_id": intValue(quiz["course"]), "cmid": intValue(quiz["coursemodule"]), "name": stringValue(quiz["name"]), "intro_html": stringValue(quiz["intro"]), "intro_text": stripHTML(stringValue(quiz["intro"])), "grade": quiz["grade"], "sumgrades": quiz["sumgrades"], "attempts_allowed": quiz["attempts"]}
	for _, key := range []string{"timeopen", "timeclose", "timelimit", "timemodified"} {
		metadata[key] = int64Value(quiz[key])
		metadata[key+"_rfc3339"] = rfc3339(int64Value(quiz[key]))
	}
	var last map[string]any
	if len(attempts) > 0 {
		last = attempts[len(attempts)-1]
	}
	return marshalResult(map[string]any{"timezone": "Europe/Madrid", "quiz": metadata, "attempt_count": len(attempts), "attempts": attempts, "last_attempt": last, "best_grade": best["grade"]})
}
