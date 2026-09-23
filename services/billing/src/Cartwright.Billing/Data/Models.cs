using System.ComponentModel.DataAnnotations;

namespace Cartwright.Billing.Data;

public class Payment
{
    [Key]
    public string Id { get; set; } = string.Empty;
    public string OrderId { get; set; } = string.Empty;
    public long AmountCents { get; set; }
    public string Currency { get; set; } = string.Empty;
    public string Status { get; set; } = string.Empty;
    public DateTime CreatedAt { get; set; } = DateTime.UtcNow;
    public DateTime UpdatedAt { get; set; } = DateTime.UtcNow;
}

public class IdempotencyRecord
{
    [Key]
    public string Key { get; set; } = string.Empty;
    public string Operation { get; set; } = string.Empty;
    public string RequestHash { get; set; } = string.Empty;
    public byte[] Response { get; set; } = [];
    public DateTime CreatedAt { get; set; } = DateTime.UtcNow;
}
