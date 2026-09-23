from google.protobuf.internal import enum_type_wrapper as _enum_type_wrapper
from google.protobuf import descriptor as _descriptor
from google.protobuf import message as _message
from typing import ClassVar as _ClassVar, Optional as _Optional, Union as _Union

DESCRIPTOR: _descriptor.FileDescriptor

class PaymentStatus(int, metaclass=_enum_type_wrapper.EnumTypeWrapper):
    __slots__ = ()
    PAYMENT_STATUS_UNSPECIFIED: _ClassVar[PaymentStatus]
    PAYMENT_STATUS_AUTHORIZED: _ClassVar[PaymentStatus]
    PAYMENT_STATUS_CAPTURED: _ClassVar[PaymentStatus]
    PAYMENT_STATUS_VOIDED: _ClassVar[PaymentStatus]
    PAYMENT_STATUS_DECLINED: _ClassVar[PaymentStatus]
PAYMENT_STATUS_UNSPECIFIED: PaymentStatus
PAYMENT_STATUS_AUTHORIZED: PaymentStatus
PAYMENT_STATUS_CAPTURED: PaymentStatus
PAYMENT_STATUS_VOIDED: PaymentStatus
PAYMENT_STATUS_DECLINED: PaymentStatus

class AuthorizeRequest(_message.Message):
    __slots__ = ("idempotency_key", "order_id", "amount_cents", "currency", "payment_token")
    IDEMPOTENCY_KEY_FIELD_NUMBER: _ClassVar[int]
    ORDER_ID_FIELD_NUMBER: _ClassVar[int]
    AMOUNT_CENTS_FIELD_NUMBER: _ClassVar[int]
    CURRENCY_FIELD_NUMBER: _ClassVar[int]
    PAYMENT_TOKEN_FIELD_NUMBER: _ClassVar[int]
    idempotency_key: str
    order_id: str
    amount_cents: int
    currency: str
    payment_token: str
    def __init__(self, idempotency_key: _Optional[str] = ..., order_id: _Optional[str] = ..., amount_cents: _Optional[int] = ..., currency: _Optional[str] = ..., payment_token: _Optional[str] = ...) -> None: ...

class AuthorizeResponse(_message.Message):
    __slots__ = ("payment_id", "status", "decline_reason")
    PAYMENT_ID_FIELD_NUMBER: _ClassVar[int]
    STATUS_FIELD_NUMBER: _ClassVar[int]
    DECLINE_REASON_FIELD_NUMBER: _ClassVar[int]
    payment_id: str
    status: PaymentStatus
    decline_reason: str
    def __init__(self, payment_id: _Optional[str] = ..., status: _Optional[_Union[PaymentStatus, str]] = ..., decline_reason: _Optional[str] = ...) -> None: ...

class CaptureRequest(_message.Message):
    __slots__ = ("idempotency_key", "order_id")
    IDEMPOTENCY_KEY_FIELD_NUMBER: _ClassVar[int]
    ORDER_ID_FIELD_NUMBER: _ClassVar[int]
    idempotency_key: str
    order_id: str
    def __init__(self, idempotency_key: _Optional[str] = ..., order_id: _Optional[str] = ...) -> None: ...

class CaptureResponse(_message.Message):
    __slots__ = ("payment_id", "status")
    PAYMENT_ID_FIELD_NUMBER: _ClassVar[int]
    STATUS_FIELD_NUMBER: _ClassVar[int]
    payment_id: str
    status: PaymentStatus
    def __init__(self, payment_id: _Optional[str] = ..., status: _Optional[_Union[PaymentStatus, str]] = ...) -> None: ...

class VoidRequest(_message.Message):
    __slots__ = ("idempotency_key", "order_id")
    IDEMPOTENCY_KEY_FIELD_NUMBER: _ClassVar[int]
    ORDER_ID_FIELD_NUMBER: _ClassVar[int]
    idempotency_key: str
    order_id: str
    def __init__(self, idempotency_key: _Optional[str] = ..., order_id: _Optional[str] = ...) -> None: ...

class VoidResponse(_message.Message):
    __slots__ = ("status",)
    STATUS_FIELD_NUMBER: _ClassVar[int]
    status: PaymentStatus
    def __init__(self, status: _Optional[_Union[PaymentStatus, str]] = ...) -> None: ...
