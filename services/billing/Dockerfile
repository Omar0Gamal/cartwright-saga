FROM mcr.microsoft.com/dotnet/sdk:8.0 AS build
WORKDIR /src

COPY ["services/billing/src/Cartwright.Billing/Cartwright.Billing.csproj", "services/billing/src/Cartwright.Billing/"]
COPY ["proto/", "/src/proto/"]

RUN dotnet restore "services/billing/src/Cartwright.Billing/Cartwright.Billing.csproj"
COPY services/billing/src/Cartwright.Billing/ services/billing/src/Cartwright.Billing/
WORKDIR "/src/services/billing/src/Cartwright.Billing"
RUN dotnet publish "Cartwright.Billing.csproj" -c Release -o /app/publish

FROM mcr.microsoft.com/dotnet/aspnet:8.0 AS final
WORKDIR /app
COPY --from=build /app/publish .
USER root
RUN apt-get update && apt-get install -y curl && rm -rf /var/lib/apt/lists/*
USER $APP_UID

EXPOSE 50051
ENTRYPOINT ["dotnet", "Cartwright.Billing.dll"]
