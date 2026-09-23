using Grpc.Core;
using Billing.V1;
using Cartwright.Billing.Data;
using Microsoft.EntityFrameworkCore;
using Google.Protobuf;
using System.Security.Cryptography;

namespace Cartwright.Billing.Services;

public class BillingServiceImpl : global::Billing.V1.Billing.BillingBase
{
    private readonly ILogger<BillingServiceImpl> _logger;
    private readonly BillingDbContext _db;
    private readonly IConfiguration _config;

    public BillingServiceImpl(ILogger<BillingServiceImpl> logger, BillingDbContext db, IConfiguration config)
    {
        _logger = logger;
        _db = db;
        _config = config;
    }

    public override async Task<AuthorizeResponse> Authorize(AuthorizeRequest request, ServerCallContext context)
    {
        _logger.LogInformation("Authorizing payment for order {OrderId}", request.OrderId);

        var (resp, isHit) = await WithIdempotencyAsync(request, request.IdempotencyKey, "Authorize", async () =>
        {
            var existingPayment = await _db.Payments.FirstOrDefaultAsync(p => p.OrderId == request.OrderId);
            if (existingPayment != null && existingPayment.Status == "Voided")
            {
                throw new RpcException(new Status(StatusCode.FailedPrecondition, "Order has been voided"));
            }

            var status = request.PaymentToken == "tok_declined" ? PaymentStatus.Declined : PaymentStatus.Authorized;
            var authResp = new AuthorizeResponse
            {
                PaymentId = Guid.NewGuid().ToString(),
                Status = status,
                DeclineReason = status == PaymentStatus.Declined ? "Card declined" : ""
            };

            if (existingPayment == null)
            {
                _db.Payments.Add(new Payment
                {
                    Id = authResp.PaymentId,
                    OrderId = request.OrderId,
                    AmountCents = request.AmountCents,
                    Currency = request.Currency,
                    Status = status.ToString()
                });
            }

            return authResp;
        });

        if (!isHit && _config["CARTWRIGHT_FAULTS"] == "1" && request.PaymentToken == "tok_fault")
        {
            _logger.LogWarning("Fault hook active — delaying response for order {OrderId}", request.OrderId);
            await Task.Delay(10000);
        }

        return resp;
    }

    public override async Task<CaptureResponse> Capture(CaptureRequest request, ServerCallContext context)
    {
        _logger.LogInformation("Capturing payment for order {OrderId}", request.OrderId);

        var (resp, _) = await WithIdempotencyAsync(request, request.IdempotencyKey, "Capture", async () =>
        {
            var p = await _db.Payments.FirstOrDefaultAsync(x => x.OrderId == request.OrderId);
            if (p == null || p.Status != "Authorized")
                throw new RpcException(new Status(StatusCode.FailedPrecondition, "Payment not in Authorized state"));

            p.Status = "Captured";
            p.UpdatedAt = DateTime.UtcNow;

            return new CaptureResponse { PaymentId = p.Id, Status = PaymentStatus.Captured };
        });

        return resp;
    }

    public override async Task<VoidResponse> Void(VoidRequest request, ServerCallContext context)
    {
        _logger.LogInformation("Voiding payment for order {OrderId}", request.OrderId);

        var (resp, _) = await WithIdempotencyAsync(request, request.IdempotencyKey, "Void", async () =>
        {
            var p = await _db.Payments.FirstOrDefaultAsync(x => x.OrderId == request.OrderId);

            if (p != null && p.Status == "Captured")
                throw new RpcException(new Status(StatusCode.FailedPrecondition, "Cannot void a captured payment"));

            if (p != null)
            {
                p.Status = "Voided";
                p.UpdatedAt = DateTime.UtcNow;
            }
            else
            {
                // Tombstone: block any future authorize for this order
                _db.Payments.Add(new Payment
                {
                    Id = Guid.NewGuid().ToString(),
                    OrderId = request.OrderId,
                    Status = "Voided"
                });
            }

            return new VoidResponse { Status = PaymentStatus.Voided };
        });

        return resp;
    }

    private async Task<(TResponse Response, bool IsIdempotencyHit)> WithIdempotencyAsync<TRequest, TResponse>(
        TRequest request,
        string idempotencyKey,
        string operationName,
        Func<Task<TResponse>> operation)
        where TRequest : IMessage
        where TResponse : class, IMessage<TResponse>, new()
    {
        try
        {
            await using var tx = await _db.Database.BeginTransactionAsync();

            var reqHash = HashRequest(request.ToByteArray());
            var existingRecord = await _db.IdempotencyRecords.FindAsync(idempotencyKey);
            if (existingRecord != null)
            {
                if (existingRecord.RequestHash != reqHash)
                    throw new RpcException(new Status(StatusCode.InvalidArgument, "Idempotency key reused with different payload"));

                _logger.LogInformation("Idempotency hit for key {Key}", idempotencyKey);
                var parser = new MessageParser<TResponse>(() => new TResponse());
                return (parser.ParseFrom(existingRecord.Response), true);
            }

            var resp = await operation();

            _db.IdempotencyRecords.Add(new IdempotencyRecord
            {
                Key = idempotencyKey,
                Operation = operationName,
                RequestHash = reqHash,
                Response = resp.ToByteArray()
            });

            await _db.SaveChangesAsync();
            await tx.CommitAsync();

            return (resp, false);
        }
        catch (RpcException)
        {
            throw;
        }
        catch (DbUpdateException)
        {
            _logger.LogWarning("Concurrent insert for key {Key}, returning ABORTED", idempotencyKey);
            throw new RpcException(new Status(StatusCode.Aborted, "Concurrent request detected"));
        }
    }

    private static string HashRequest(byte[] data)
    {
        var hash = SHA256.HashData(data);
        return Convert.ToHexString(hash).ToLowerInvariant();
    }
}
