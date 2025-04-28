# 1 Start (or re-use) Minikube
minikube start

# 2 Run your program in one terminal
go run main.go   # you should see the existing system pods being logged

# 3 In a second terminal, poke each resource type once
kubectl create deploy e2e-deploy --image=nginx          # Deployment (+ReplicaSet+Pod)
kubectl scale deploy e2e-deploy --replicas=2            # Pod UPDATEs
kubectl create configmap e2e-cm --from-literal=k=v      # ConfigMap ADD
kubectl delete configmap e2e-cm                         # ConfigMap DELETE
kubectl expose deploy e2e-deploy --port 80              # Service ADD
kubectl delete svc e2e-deploy                           # Service DELETE
kubectl delete deploy e2e-deploy                        # ReplicaSet + Pod DELETEs
