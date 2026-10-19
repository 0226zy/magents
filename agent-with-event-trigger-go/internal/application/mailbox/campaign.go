package mailbox

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/0226zy/magents/agent-with-event-trigger-go/internal/domain/mailbox"
)

// campaignMessageCount 是实验要求的三封邮件，对应三个场景。
const campaignMessageCount = 3

var emailAddressPattern = regexp.MustCompile(`^[^@\s]+@[^@\s]+\.[^@\s]+$`)

// seedMessages 发出三封带 campaign 标记的邮件。
// 标记让轮询能在共享邮箱里精确认出本次运行的邮件，不会误收别人的信。
func (s *ExperimentService) seedMessages(ctx context.Context, senderID, recipient,
	campaignID string) ([]mailbox.ProviderObject, error) {

	start := s.now().UTC().Add(48 * time.Hour).Truncate(time.Hour)
	start = time.Date(start.Year(), start.Month(), start.Day(), 10, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)

	orderSuffix := campaignID
	if len(orderSuffix) > 8 {
		orderSuffix = orderSuffix[len(orderSuffix)-8:]
	}

	messages := []struct{ subject, body string }{
		{
			fmt.Sprintf("[EXP6-1 %s] Meeting invitation: design review", campaignID),
			"Meeting invitation for the agent experiment.\n" +
				fmt.Sprintf("START_UTC: %s\nEND_UTC: %s\n", mailbox.IsoMillis(start), mailbox.IsoMillis(end)) +
				"Please accept if the calendar is free, otherwise decline.",
		},
		{
			fmt.Sprintf("[EXP6-1 %s] Customer complaint: delayed order", campaignID),
			fmt.Sprintf("Customer complaint for order #%s. The delivery is overdue and "+
				"support has not resolved it. Please arrange urgent human follow-up.", orderSuffix),
		},
		{
			fmt.Sprintf("[EXP6-1 %s] Marketing newsletter", campaignID),
			"Marketing newsletter: save 20 percent on productivity software. " +
				"This bulk promotion includes an unsubscribe link.",
		},
	}

	receipts := make([]mailbox.ProviderObject, 0, len(messages))
	for _, message := range messages {
		receipt, err := s.client.SendEmail(ctx, senderID, recipient, message.subject, message.body)
		if err != nil {
			return nil, fmt.Errorf("投递实验邮件失败: %w", err)
		}
		receipts = append(receipts, receipt)
	}
	s.log.Info("已投递 %d 封实验邮件至 campaign %s", len(receipts), campaignID)
	return receipts, nil
}

// pollCampaignEmails 轮询收件箱，直到三封带标记的邮件全部到达。
func (s *ExperimentService) pollCampaignEmails(ctx context.Context, accountID, campaignID string,
	after time.Time, timeout, interval time.Duration) ([]mailbox.ProviderObject, error) {

	if timeout <= 0 {
		timeout = 3 * time.Minute
	}
	if interval <= 0 {
		interval = 5 * time.Second
	}

	folder := s.inboxFolder(ctx, accountID)
	marker := fmt.Sprintf("[EXP6-1 %s]", campaignID)
	found := map[string]mailbox.ProviderObject{}
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) && len(found) < campaignMessageCount {
		references, err := s.client.ListEmails(ctx, accountID, after, folder, 100)
		if err != nil {
			return nil, err
		}
		for _, reference := range references {
			if !strings.Contains(mailbox.StringField(reference, "subject"), marker) {
				continue
			}
			id := mailbox.StringField(reference, "id")
			if id == "" {
				continue
			}
			if _, seen := found[id]; seen {
				continue
			}
			full, err := s.client.GetEmail(ctx, id)
			if err != nil {
				return nil, err
			}
			// 跳过发件箱里的回声：我们要的是真正入站的那一封。
			if strings.EqualFold(mailbox.StringField(full, "role"), "sent") {
				continue
			}
			found[id] = full
		}
		if len(found) < campaignMessageCount {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(interval):
			}
		}
	}

	if len(found) != campaignMessageCount {
		return nil, fmt.Errorf("超时前只收到 %d/%d 封实验邮件", len(found), campaignMessageCount)
	}

	emails := make([]mailbox.ProviderObject, 0, len(found))
	for _, email := range found {
		emails = append(emails, email)
	}
	sort.Slice(emails, func(i, j int) bool { return mailbox.ProviderOrder(emails[i], emails[j]) })
	return emails, nil
}

// inboxFolder 找出收件箱 folder 标识；找不到时返回空串，由 provider 用默认值。
func (s *ExperimentService) inboxFolder(ctx context.Context, accountID string) string {
	folders, err := s.client.ListFolders(ctx, accountID)
	if err != nil {
		return ""
	}
	for _, folder := range folders {
		if !strings.EqualFold(mailbox.StringField(folder, "role"), "inbox") &&
			!strings.EqualFold(mailbox.StringField(folder, "name"), "inbox") {
			continue
		}
		for _, key := range []string{"provider_id", "id", "name"} {
			if value := mailbox.StringField(folder, key); value != "" {
				return value
			}
		}
	}
	return ""
}

// chooseAccount 选出要使用的账号：指定 ID 时精确匹配，否则取第一个有邮件能力的。
func chooseAccount(accounts []mailbox.ProviderObject, requested string) (mailbox.ProviderObject, error) {
	if requested != "" {
		for _, account := range accounts {
			if mailbox.StringField(account, "id") == requested {
				return account, nil
			}
		}
		return nil, fmt.Errorf("Unipile 未返回请求的账号 ID: %s", requested)
	}
	for _, account := range accounts {
		if strings.Contains(strings.ToLower(mailbox.CanonicalJSONString(account["sources"])), "mail") ||
			accountEmail(account) != "" {
			return account, nil
		}
	}
	return nil, errors.New("Unipile 未返回任何具备邮件能力的账号")
}

// accountEmail 在账号对象里找一个邮箱形状的标识。
func accountEmail(account mailbox.ProviderObject) string {
	for _, key := range []string{"email", "identifier", "username", "user", "name"} {
		if value, ok := account[key].(string); ok && emailAddressPattern.MatchString(value) {
			return value
		}
	}
	for _, value := range account {
		if nested, ok := value.(map[string]any); ok {
			if found := accountEmail(nested); found != "" {
				return found
			}
		}
	}
	return ""
}
